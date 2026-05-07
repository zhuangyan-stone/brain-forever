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

	"BrainOnline/infra/httpx"
)

// ============================================================
// ZhiPu Web Search API Client
// API Docs: https://open.bigmodel.cn/dev/api/search
// ============================================================

const (
	// ZhiPuDefaultBaseURL is the default endpoint for ZhiPu Web Search API
	ZhiPuDefaultBaseURL = "https://open.bigmodel.cn/api/paas/v4/web_search"

	// ZhiPuDefaultTimeout is the default HTTP request timeout
	ZhiPuDefaultTimeout = 30 * time.Second
)

// ---------------------------------------------------------------------------
// Request types (internal — used for JSON serialization to the ZhiPu API)
// ---------------------------------------------------------------------------

// zhipuWebSearchRequest is the request body sent to the ZhiPu Web Search API.
type zhipuWebSearchRequest struct {
	SearchQuery         string `json:"search_query"`
	SearchEngine        string `json:"search_engine"`
	SearchIntent        bool   `json:"search_intent"`
	Count               int    `json:"count"`
	SearchDomainFilter  string `json:"search_domain_filter,omitempty"`
	SearchRecencyFilter string `json:"search_recency_filter,omitempty"`
	ContentSize         string `json:"content_size"`
	RequestID           string `json:"request_id,omitempty"`
	UserID              string `json:"user_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Response types (internal — used for JSON deserialization from the ZhiPu API)
// ---------------------------------------------------------------------------

// zhipuWebSearchResult is the top-level response from the ZhiPu Web Search API.
type zhipuWebSearchResult struct {
	ID           string                  `json:"id"`
	Created      int64                   `json:"created"`
	RequestID    string                  `json:"request_id"`
	SearchIntent []zhipuSearchIntent     `json:"search_intent"`
	SearchResult []zhipuSearchResultItem `json:"search_result"`
}

// zhipuSearchIntent represents a search intent item in the response.
type zhipuSearchIntent struct {
	Query    string `json:"query"`
	Intent   string `json:"intent"`
	Keywords string `json:"keywords"`
}

// zhipuSearchResultItem represents a single web page result.
type zhipuSearchResultItem struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	Link        string `json:"link"`
	Media       string `json:"media"` // site name
	Icon        string `json:"icon"`
	Refer       string `json:"refer"`
	PublishDate string `json:"publish_date"`
}

// ============================================================
// zhiPuClient
// ============================================================

// zhiPuClient is the ZhiPu Web Search API client.
// It implements the WebSearcher interface.
type zhiPuClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// compile-time check: *zhiPuClient implements WebSearcher
var _ WebSearcher = (*zhiPuClient)(nil)

// NewZhiPuClient creates a new ZhiPu Web Search API client.
// If cfg.BaseURL is empty, ZhiPuDefaultBaseURL is used.
// If cfg.HTTPClient is nil, a default client with 30s timeout is created.
func NewZhiPuClient(cfg WebSearchClientConfig) *zhiPuClient {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = ZhiPuDefaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httpx.NewHTTPClient(ZhiPuDefaultTimeout)
	}

	return &zhiPuClient{
		apiKey:     cfg.APIKey,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// Search implements the WebSearcher interface.
// It sends a web search request to the ZhiPu API and returns the parsed response.
func (c *zhiPuClient) Search(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	return c.searchCore(ctx, req)
}

// SearchForLLM implements the WebSearcher interface.
// It performs a web search and returns both the parsed response and an
// LLM-friendly formatted text.
func (c *zhiPuClient) SearchForLLM(ctx context.Context, req WebSearchRequest, maxRuneLen int) (*WebSearchResponse, string, error) {
	result, err := c.searchCore(ctx, req)
	if err != nil {
		return nil, "", err
	}

	llmText := ResultToLLMText(result, maxRuneLen)
	return result, llmText, nil
}

// searchCore performs the actual HTTP request to the ZhiPu API and returns
// the parsed response.
func (c *zhiPuClient) searchCore(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error) {
	queryStr := strings.Join(req.Query, " ")

	// Map the generic Freshness to ZhiPu's recency filter
	recencyFilter := mapFreshnessToZhiPuRecency(req.Freshness)

	zReq := zhipuWebSearchRequest{
		SearchQuery:         queryStr,
		SearchEngine:        "search_std",
		SearchIntent:        false,
		Count:               req.Count,
		SearchDomainFilter:  req.Include,
		SearchRecencyFilter: recencyFilter,
		ContentSize:         "medium",
		RequestID:           "",
		UserID:              "brain-forever-001",
	}

	bodyBytes, err := json.Marshal(zReq)
	if err != nil {
		return nil, fmt.Errorf("zhipu. failed to marshal request. %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("zhipu. failed to create request. %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("zhipu. request failed. %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("zhipu. failed to read response body. %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("zhipu. unexpected status code %d. body: %s", resp.StatusCode, string(respBody))
	}

	var zResp zhipuWebSearchResult
	if err := json.Unmarshal(respBody, &zResp); err != nil {
		return nil, fmt.Errorf("zhipu. failed to unmarshal response. %w", err)
	}

	// Convert internal response to interface response
	result := convertZhiPuToSearchResponse(&zResp)

	return result, nil
}

// ============================================================
// Conversion helpers
// ============================================================

// convertZhiPuToSearchResponse converts a zhipuWebSearchResult to a WebSearchResponse.
func convertZhiPuToSearchResponse(z *zhipuWebSearchResult) *WebSearchResponse {
	if z == nil {
		return nil
	}

	result := &WebSearchResponse{}

	if len(z.SearchResult) > 0 {
		pages := make([]WebPageValue, 0, len(z.SearchResult))
		for _, v := range z.SearchResult {
			pages = append(pages, WebPageValue{
				ID:          v.Refer,
				Name:        v.Title,
				URL:         v.Link,
				Summary:     v.Content,
				SiteName:    v.Media,
				SiteIcon:    v.Icon,
				PublishDate: v.PublishDate,
			})
		}
		result.Pages = pages
	}

	return result
}

// mapFreshnessToZhiPuRecency maps the generic Freshness value to ZhiPu's
// search_recency_filter format.
//
// Supported values:
//   - noLimit (default)
//   - oneDay
//   - oneWeek
//   - oneMonth
//   - oneYear
//
// Note: ZhiPu does not support the YYYY-MM-DD..YYYY-MM-DD or YYYY-MM-DD formats.
// If an unsupported format is provided, it falls back to "noLimit".
func mapFreshnessToZhiPuRecency(freshness string) string {
	switch freshness {
	case "noLimit", "oneDay", "oneWeek", "oneMonth", "oneYear":
		return freshness
	default:
		return "noLimit"
	}
}
