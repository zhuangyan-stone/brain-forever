package bocha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"BrainOnline/infra/3rdapi/search"
	"BrainOnline/infra/httpx"
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
	WebSearchURL       *string           `json:"webSearchUrl"`
	Name               *string           `json:"name"`
	ThumbnailURL       string            `json:"thumbnailUrl"`
	DatePublished      *string           `json:"datePublished"`
	ContentURL         string            `json:"contentUrl"`
	HostPageURL        string            `json:"hostPageUrl"`
	ContentSize        *string           `json:"contentSize"`
	EncodingFormat     *string           `json:"encodingFormat"`
	HostPageDisplayURL string            `json:"hostPageDisplayUrl"`
	Width              int               `json:"width"`
	Height             int               `json:"height"`
	Thumbnail          *search.Thumbnail `json:"thumbnail"`
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
	WebSearchURL       string             `json:"webSearchUrl"`
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	ThumbnailURL       string             `json:"thumbnailUrl"`
	Publisher          []search.Publisher `json:"publisher"`
	Creator            *search.Creator    `json:"creator"`
	ContentURL         string             `json:"contentUrl"`
	HostPageURL        string             `json:"hostPageUrl"`
	EncodingFormat     string             `json:"encodingFormat"`
	HostPageDisplayURL string             `json:"hostPageDisplayUrl"`
	Width              int                `json:"width"`
	Height             int                `json:"height"`
	Duration           string             `json:"duration"`
	MotionThumbnailURL string             `json:"motionThumbnailUrl"`
	EmbedHTML          string             `json:"embedHtml"`
	AllowHttpsEmbed    bool               `json:"allowHttpsEmbed"`
	ViewCount          int                `json:"viewCount"`
	Thumbnail          *search.Thumbnail  `json:"thumbnail"`
	AllowMobileEmbed   bool               `json:"allowMobileEmbed"`
	IsSuperfresh       bool               `json:"isSuperfresh"`
	DatePublished      string             `json:"datePublished"`
}

// ============================================================
// Client
// ============================================================

// Client is the Bocha Web Search API client.
// It implements the search.WebSearcher interface.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// compile-time check: *Client implements search.WebSearcher
var _ search.WebSearcher = (*Client)(nil)

// NewClient creates a new Bocha Web Search API client.
// If cfg.BaseURL is empty, DefaultBaseURL is used.
// If cfg.HTTPClient is nil, a default client with 30s timeout is created.
func NewClient(cfg search.WebSearchClientConfig) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httpx.NewHTTPClient(DefaultTimeout)
	}

	return &Client{
		apiKey:     cfg.APIKey,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// Search implements the search.WebSearcher interface.
// It sends a web search request to the Bocha API and returns the parsed response.
// When req.FamilyFriendlyOnly is true, results that are not family-friendly
// (web pages, images, videos) are filtered out.
func (c *Client) Search(ctx context.Context, req search.WebSearchRequest) (*search.WebSearchResponse, error) {
	result, err := c.searchCore(ctx, req)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SearchForLLM implements the search.WebSearcher interface.
// It performs a web search and returns both the parsed response and an
// LLM-friendly formatted text.
func (c *Client) SearchForLLM(ctx context.Context, req search.WebSearchRequest, maxWebPages, maxImages, maxVideos, maxRuneLen int) (*search.WebSearchResponse, string, error) {
	result, err := c.searchCore(ctx, req)
	if err != nil {
		return nil, "", err
	}

	llmText := search.FormatSearchResultToLLMText(result, maxWebPages, maxImages, maxVideos, maxRuneLen)
	return result, llmText, nil
}

// searchCore performs the actual HTTP request to the Bocha API and returns
// the parsed response, with family-friendly filtering applied if requested.
func (c *Client) searchCore(ctx context.Context, req search.WebSearchRequest) (*search.WebSearchResponse, error) {
	queryStr := strings.Join(req.Query, " ")

	// Join multiple query terms: each term wrapped in double quotes, joined by space.
	// e.g. ["term1", "term2"] -> `"term1" "term2"`
	// for i, q := range req.Query {
	// 	if i > 0 {
	// 		queryStr += " "
	// 	}
	// 	queryStr += `"` + q + `"`
	// }

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
		filterNonFamilyFriendly(result)
	}

	return result, nil
}

// ============================================================
// Conversion helpers
// ============================================================

// convertToSearchResponse converts a bochaResponse to a search.WebSearchResponse.
func convertToSearchResponse(b *bochaResponse) *search.WebSearchResponse {
	if b == nil {
		return nil
	}

	result := &search.WebSearchResponse{
		Code:  b.Code,
		LogID: b.LogID,
		Msg:   b.Msg,
	}

	if b.Data != nil {
		result.Data = &search.WebSearchData{
			OriginalQuery: b.Data.QueryContext.OriginalQuery,
		}

		// Convert WebPages
		if b.Data.WebPages != nil {
			wp := &search.WebSearchWebPages{
				WebSearchURL:          b.Data.WebPages.WebSearchURL,
				TotalEstimatedMatches: b.Data.WebPages.TotalEstimatedMatches,
				SomeResultsRemoved:    b.Data.WebPages.SomeResultsRemoved,
				Value:                 make([]search.WebPageValue, len(b.Data.WebPages.Value)),
			}
			for i, v := range b.Data.WebPages.Value {
				wp.Value[i] = search.WebPageValue{
					ID:               v.ID,
					Name:             v.Name,
					URL:              v.URL,
					DisplayURL:       v.DisplayURL,
					Snippet:          v.Snippet,
					Summary:          v.Summary,
					SiteName:         v.SiteName,
					SiteIcon:         v.SiteIcon,
					DatePublished:    v.DatePublished,
					CachedPageURL:    v.CachedPageURL,
					Language:         v.Language,
					IsFamilyFriendly: v.IsFamilyFriendly,
				}
			}
			result.Data.WebPages = wp
		}

		// Convert Images
		if b.Data.Images != nil {
			imgs := &search.WebSearchImages{
				ID:               b.Data.Images.ID,
				ReadLink:         b.Data.Images.ReadLink,
				WebSearchURL:     b.Data.Images.WebSearchURL,
				IsFamilyFriendly: b.Data.Images.IsFamilyFriendly,
				Value:            make([]search.ImageValue, len(b.Data.Images.Value)),
			}
			for i, v := range b.Data.Images.Value {
				imgs.Value[i] = search.ImageValue{
					WebSearchURL:       v.WebSearchURL,
					Name:               v.Name,
					ThumbnailURL:       v.ThumbnailURL,
					DatePublished:      v.DatePublished,
					ContentURL:         v.ContentURL,
					HostPageURL:        v.HostPageURL,
					ContentSize:        v.ContentSize,
					EncodingFormat:     v.EncodingFormat,
					HostPageDisplayURL: v.HostPageDisplayURL,
					Width:              v.Width,
					Height:             v.Height,
					Thumbnail:          v.Thumbnail,
					// Note: bocha API does not provide IsFamilyFriendly per image,
					// so it remains nil (treated as family-friendly by default).
				}
			}
			result.Data.Images = imgs
		}

		// Convert Videos
		if b.Data.Videos != nil {
			vids := &search.WebSearchVideos{
				ID:               b.Data.Videos.ID,
				ReadLink:         b.Data.Videos.ReadLink,
				WebSearchURL:     b.Data.Videos.WebSearchURL,
				IsFamilyFriendly: b.Data.Videos.IsFamilyFriendly,
				Scenario:         b.Data.Videos.Scenario,
				Value:            make([]search.VideoValue, len(b.Data.Videos.Value)),
			}
			for i, v := range b.Data.Videos.Value {
				vids.Value[i] = search.VideoValue{
					WebSearchURL:       v.WebSearchURL,
					Name:               v.Name,
					Description:        v.Description,
					ThumbnailURL:       v.ThumbnailURL,
					Publisher:          v.Publisher,
					Creator:            v.Creator,
					ContentURL:         v.ContentURL,
					HostPageURL:        v.HostPageURL,
					EncodingFormat:     v.EncodingFormat,
					HostPageDisplayURL: v.HostPageDisplayURL,
					Width:              v.Width,
					Height:             v.Height,
					Duration:           v.Duration,
					MotionThumbnailURL: v.MotionThumbnailURL,
					EmbedHTML:          v.EmbedHTML,
					AllowHttpsEmbed:    v.AllowHttpsEmbed,
					ViewCount:          v.ViewCount,
					Thumbnail:          v.Thumbnail,
					AllowMobileEmbed:   v.AllowMobileEmbed,
					IsSuperfresh:       v.IsSuperfresh,
					DatePublished:      v.DatePublished,
					// Note: bocha API does not provide IsFamilyFriendly per video,
					// so it remains nil (treated as family-friendly by default).
				}
			}
			result.Data.Videos = vids
		}
	}

	return result
}

// filterNonFamilyFriendly removes results that are not family-friendly in-place.
//
// For web pages, each item has its own IsFamilyFriendly field, so we filter
// at the item level.
//
// For images and videos, the Bocha API only provides IsFamilyFriendly at the
// container level (WebSearchImages / WebSearchVideos). If the container is
// explicitly marked as not family-friendly, we clear the entire value list.
func filterNonFamilyFriendly(resp *search.WebSearchResponse) {
	if resp == nil || resp.Data == nil {
		return
	}

	// Filter web pages (per-item IsFamilyFriendly is available)
	if resp.Data.WebPages != nil {
		filtered := make([]search.WebPageValue, 0, len(resp.Data.WebPages.Value))
		for _, v := range resp.Data.WebPages.Value {
			if v.IsFamilyFriendly == nil || *v.IsFamilyFriendly {
				filtered = append(filtered, v)
			}
		}
		resp.Data.WebPages.Value = filtered
	}

	// Filter images: only container-level IsFamilyFriendly is available.
	// If the container is explicitly not family-friendly, clear all images.
	if resp.Data.Images != nil {
		if resp.Data.Images.IsFamilyFriendly != nil && !*resp.Data.Images.IsFamilyFriendly {
			resp.Data.Images.Value = nil
		}
	}

	// Filter videos: only container-level IsFamilyFriendly is available.
	// If the container is explicitly not family-friendly, clear all videos.
	if resp.Data.Videos != nil {
		if resp.Data.Videos.IsFamilyFriendly != nil && !*resp.Data.Videos.IsFamilyFriendly {
			resp.Data.Videos.Value = nil
		}
	}
}
