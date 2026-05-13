package searcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"BrainForever/infra/httpx"
	"BrainForever/toolset"
)

// ============================================================
// Bocha Web Search API Client
// API Docs: https://api.bocha.cn/v1/web-search
// ============================================================

const (
	// DefaultBaseURL is the default endpoint for Bocha Web Search API
	DefaultBaseURL = "https://api.bocha.cn/v1/web-search"

	// DefaultTimeout is the default HTTP request timeout
	DefaultTimeout = 30 * time.Second
)

// ---------------------------------------------------------------------------
// Request types (internal — used for JSON serialization to the Bocha API)
// ---------------------------------------------------------------------------

// bochaRequest is the request body sent to the Bocha API.
type bochaRequest struct {
	Query     string `json:"query"`
	Freshness string `json:"freshness,omitempty"`
	Summary   bool   `json:"summary,omitempty"`
	Include   string `json:"include,omitempty"`
	Exclude   string `json:"exclude,omitempty"`
	Count     int    `json:"count,omitempty"`
}

// ---------------------------------------------------------------------------
// Response types (internal — used for JSON deserialization from the Bocha API)
// ---------------------------------------------------------------------------

// bochaResponse is the top-level response from the Bocha Web Search API.
type bochaResponse struct {
	Code  int        `json:"code"`
	LogID string     `json:"log_id"`
	Msg   *string    `json:"msg"`
	Data  *bochaData `json:"data"`
}

// bochaData is the "data" field in the response.
type bochaData struct {
	Type         string            `json:"_type"`
	QueryContext bochaQueryContext `json:"queryContext"`
	WebPages     *bochaWebPages    `json:"webPages"`
	Images       *bochaImages      `json:"images"`
	Videos       *bochaVideos      `json:"videos"`
}

// bochaQueryContext contains the original search query.
type bochaQueryContext struct {
	OriginalQuery string `json:"originalQuery"`
}

// ---------------------------------------------------------------------------
// WebPages
// ---------------------------------------------------------------------------

// bochaWebPages contains the web page search results.
type bochaWebPages struct {
	WebSearchURL          string              `json:"webSearchUrl"`
	TotalEstimatedMatches int                 `json:"totalEstimatedMatches"`
	Value                 []bochaWebPageValue `json:"value"`
	SomeResultsRemoved    bool                `json:"someResultsRemoved"`
}

// bochaWebPageValue represents a single web page result.
type bochaWebPageValue struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	URL              string  `json:"url"`
	DisplayURL       string  `json:"displayUrl"`
	Snippet          string  `json:"snippet"`
	Summary          string  `json:"summary,omitempty"`
	SiteName         string  `json:"siteName"`
	SiteIcon         string  `json:"siteIcon"`
	DatePublished    string  `json:"datePublished,omitempty"`
	DateLastCrawled  string  `json:"dateLastCrawled"`
	CachedPageURL    *string `json:"cachedPageUrl"`
	Language         *string `json:"language"`
	IsFamilyFriendly *bool   `json:"isFamilyFriendly"`
	IsNavigational   *bool   `json:"isNavigational"`
}

// ---------------------------------------------------------------------------
// Images
// ---------------------------------------------------------------------------

// bochaImages contains the image search results.
type bochaImages struct {
	ID               string            `json:"id"`
	ReadLink         *string           `json:"readLink"`
	WebSearchURL     *string           `json:"webSearchUrl"`
	IsFamilyFriendly *bool             `json:"isFamilyFriendly"`
	Value            []bochaImageValue `json:"value"`
}

// bochaImageValue represents a single image result.
type bochaImageValue struct {
	WebSearchURL       *string         `json:"webSearchUrl"`
	Name               *string         `json:"name"`
	ThumbnailURL       string          `json:"thumbnailUrl"`
	DatePublished      *string         `json:"datePublished"`
	ContentURL         string          `json:"contentUrl"`
	HostPageURL        string          `json:"hostPageUrl"`
	ContentSize        *string         `json:"contentSize"`
	EncodingFormat     *string         `json:"encodingFormat"`
	HostPageDisplayURL string          `json:"hostPageDisplayUrl"`
	Width              int             `json:"width"`
	Height             int             `json:"height"`
	Thumbnail          *bochaThumbnail `json:"thumbnail"`
}

// ---------------------------------------------------------------------------
// Videos
// ---------------------------------------------------------------------------

// bochaVideos contains the video search results.
type bochaVideos struct {
	ID               string            `json:"id"`
	ReadLink         string            `json:"readLink"`
	WebSearchURL     string            `json:"webSearchUrl"`
	IsFamilyFriendly *bool             `json:"isFamilyFriendly"`
	Scenario         string            `json:"scenario"`
	Value            []bochaVideoValue `json:"value"`
}

// bochaVideoValue represents a single video result.
type bochaVideoValue struct {
	WebSearchURL       string           `json:"webSearchUrl"`
	Name               string           `json:"name"`
	Description        string           `json:"description"`
	ThumbnailURL       string           `json:"thumbnailUrl"`
	Publisher          []bochaPublisher `json:"publisher"`
	Creator            *bochaCreator    `json:"creator"`
	ContentURL         string           `json:"contentUrl"`
	HostPageURL        string           `json:"hostPageUrl"`
	EncodingFormat     string           `json:"encodingFormat"`
	HostPageDisplayURL string           `json:"hostPageDisplayUrl"`
	Width              int              `json:"width"`
	Height             int              `json:"height"`
	Duration           string           `json:"duration"`
	MotionThumbnailURL string           `json:"motionThumbnailUrl"`
	EmbedHTML          string           `json:"embedHtml"`
	AllowHttpsEmbed    bool             `json:"allowHttpsEmbed"`
	ViewCount          int              `json:"viewCount"`
	Thumbnail          *bochaThumbnail  `json:"thumbnail"`
	AllowMobileEmbed   bool             `json:"allowMobileEmbed"`
	IsSuperfresh       bool             `json:"isSuperfresh"`
	DatePublished      string           `json:"datePublished"`
}

// bochaCreator represents the creator of a video.
type bochaCreator struct {
	Name string `json:"name"`
}

// bochaPublisher represents the publisher of a video.
type bochaPublisher struct {
	Name string `json:"name"`
}

// bochaThumbnail represents thumbnail dimensions.
type bochaThumbnail struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

// ============================================================
// bochaClient
// ============================================================

// bochaClient is the Bocha Web Search API client.
// It implements the WebSearcher interface.
type bochaClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// compile-time check: *bochaClient implements WebSearcher
var _ WebSearcher = (*bochaClient)(nil)

// NewBochaClient creates a new Bocha Web Search API client.
// If cfg.BaseURL is empty, DefaultBaseURL is used.
// If cfg.HTTPClient is nil, a default client with 30s timeout is created.
func NewBochaClient(cfg WebSearchClientConfig) *bochaClient {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httpx.NewHTTPClient(DefaultTimeout)
	}

	return &bochaClient{
		apiKey:     cfg.APIKey,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// Search implements the WebSearcher interface.
// It sends a web search request to the Bocha API and returns the parsed response.
// When req.FamilyFriendlyOnly is true, results that are not family-friendly
// (web pages, images, videos) are filtered out.
func (c *bochaClient) Search(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	return c.searchCore(ctx, req)
}

// SearchForLLM implements the WebSearcher interface.
// It performs a web search and returns both the parsed response and an
// LLM-friendly formatted text.
func (c *bochaClient) SearchForLLM(ctx context.Context, req WebSearchRequest, maxRuneLen int) (*WebSearchResponse, string, error) {
	result, err := c.searchCore(ctx, req)
	if err != nil {
		return nil, "", err
	}

	llmText := ResultToLLMText(result, maxRuneLen)
	return result, llmText, nil
}

// searchCore performs the actual HTTP request to the Bocha API and returns
// the parsed response, with family-friendly filtering applied if requested.
func (c *bochaClient) searchCore(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	queryStr := strings.Join(req.Query, " ")

	bReq := bochaRequest{
		Query:     queryStr,
		Freshness: req.Freshness,
		Summary:   req.Summary,
		Include:   req.Include,
		Exclude:   req.Exclude,
		Count:     req.Count,
	}

	bodyBytes, err := json.Marshal(bReq)
	if err != nil {
		return nil, fmt.Errorf("bocha. failed to marshal request. %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("bocha. failed to create request. %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bocha. request failed. %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bocha. failed to read response body. %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bocha. unexpected status code %d. body: %s", resp.StatusCode, string(respBody))
	}

	var bResp bochaResponse
	if err := json.Unmarshal(respBody, &bResp); err != nil {
		return nil, fmt.Errorf("bocha. failed to unmarshal response. %w", err)
	}

	if bResp.Code != 200 {
		msg := ""
		if bResp.Msg != nil {
			msg = *bResp.Msg
		}
		return nil, fmt.Errorf("bocha. API error code=%d, msg=%s, log_id=%s", bResp.Code, msg, bResp.LogID)
	}

	// Convert internal response to interface response
	result := convertToSearchResponse(&bResp)

	// Apply family-friendly filter if requested
	if req.FamilyFriendlyOnly {
		filterBochaNonFamilyFriendly(result)
	}

	return result, nil
}

// ============================================================
// Conversion helpers
// ============================================================

// convertToSearchResponse converts a bochaResponse to a WebSearchResponse.
func convertToSearchResponse(b *bochaResponse) *WebSearchResponse {
	if b == nil {
		return nil
	}

	result := &WebSearchResponse{}

	if b.Data != nil && b.Data.WebPages != nil {
		pages := make([]WebPageValue, 0, len(b.Data.WebPages.Value))
		for _, v := range b.Data.WebPages.Value {
			t, err := toolset.TryParseTimeString(v.DatePublished, time.RFC3339)
			if err != nil {
				fmt.Printf("parse bochat-web-search date_published fail. %v\n", err)
			}

			pages = append(pages, WebPageValue{
				ID:               v.ID,
				Title:            v.Name,
				URL:              v.URL,
				Snippet:          v.Snippet,
				Summary:          v.Summary,
				SiteName:         v.SiteName,
				SiteIcon:         v.SiteIcon,
				Language:         v.Language,
				PublishDate:      t,
				IsFamilyFriendly: v.IsFamilyFriendly,
			})
		}
		result.Pages = pages
	}

	return result
}

// filterBochaNonFamilyFriendly removes results that are not family-friendly in-place.
func filterBochaNonFamilyFriendly(resp *WebSearchResponse) {
	if resp == nil || len(resp.Pages) == 0 {
		return
	}

	unsupported := true

	for _, v := range resp.Pages {
		if v.IsFamilyFriendly != nil {
			unsupported = false
			break
		}
	}

	if unsupported {
		return
	}

	filtered := make([]WebPageValue, 0, len(resp.Pages))
	for _, v := range resp.Pages {
		if v.IsFamilyFriendly == nil || *v.IsFamilyFriendly {
			filtered = append(filtered, v)
		}
	}
	resp.Pages = filtered
}
