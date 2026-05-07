package search

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// WebSearcher is a generic Web search interface.
//
// Search performs a web search and returns the complete search result structure.
// SearchForLLM performs a search and additionally returns a text formatted as
// LLM-friendly input.
//
// maxWebPages / maxImages / maxVideos control the maximum number of each result
// type in the output;
// maxRuneLen controls the maximum rune length of the returned LLM text; when
// exceeded, the text is truncated with "...".
type WebSearcher interface {
	Search(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error)

	SearchForLLM(ctx context.Context, req WebSearchRequest, maxWebPages, maxImages, maxVideos, maxRuneLen int) (*WebSearchResponse, string, error)
}

// ---------------------------------------------------------------------------
// ClientConfig — Generic Web Search client configuration
// ---------------------------------------------------------------------------

// WebSearchClientConfig holds the common configuration for creating a
// Web Search API client. Each implementation (bocha, etc.) embeds or
// uses this struct to ensure consistent configuration fields.
type WebSearchClientConfig struct {
	APIKey     string       // API key for the search service
	BaseURL    string       // API base URL (default varies by implementation)
	HTTPClient *http.Client // Optional custom HTTP client; nil uses default
}

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

// WebSearchRequest represents the request body for the Web Search API.
type WebSearchRequest struct {
	Query              []string `json:"query"`
	Freshness          string   `json:"freshness,omitempty"`
	Summary            bool     `json:"summary,omitempty"`
	Include            string   `json:"include,omitempty"`
	Exclude            string   `json:"exclude,omitempty"`
	Count              int      `json:"count,omitempty"`
	FamilyFriendlyOnly bool     `json:"family_friendly_only"`
}

// ---------------------------------------------------------------------------
// Response types (top-level)
// ---------------------------------------------------------------------------

// WebSearchResponse is the top-level response from the Bocha Web Search API.
type WebSearchResponse struct {
	Code  int            `json:"code"`
	LogID string         `json:"log_id"`
	Msg   *string        `json:"msg"`
	Data  *WebSearchData `json:"data"`
}

// WebSearchData is the "data" field in the response.
type WebSearchData struct {
	OriginalQuery string `json:"originalQuery"`

	WebPages *WebSearchWebPages `json:"webPages"`
	Images   *WebSearchImages   `json:"images"`
	Videos   *WebSearchVideos   `json:"videos"`
}

// ---------------------------------------------------------------------------
// WebPages
// ---------------------------------------------------------------------------

// WebSearchWebPages contains the web page search results.
type WebSearchWebPages struct {
	WebSearchURL string `json:"webSearchUrl"`

	TotalEstimatedMatches int            `json:"totalEstimatedMatches"`
	Value                 []WebPageValue `json:"value"`

	SomeResultsRemoved bool `json:"someResultsRemoved"`
}

// WebPageValue represents a single web page result.
type WebPageValue struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	URL              string  `json:"url"`
	DisplayURL       string  `json:"displayUrl"`
	Snippet          string  `json:"snippet"`
	Summary          string  `json:"summary,omitempty"`
	SiteName         string  `json:"siteName"`
	SiteIcon         string  `json:"siteIcon"`
	DatePublished    string  `json:"datePublished,omitempty"`
	CachedPageURL    *string `json:"cachedPageUrl"`
	Language         *string `json:"language"`
	IsFamilyFriendly *bool   `json:"isFamilyFriendly"`
}

// ---------------------------------------------------------------------------
// Images
// ---------------------------------------------------------------------------

// WebSearchImages contains the image search results.
type WebSearchImages struct {
	ID               string       `json:"id"`
	ReadLink         *string      `json:"readLink"`
	WebSearchURL     *string      `json:"webSearchUrl"`
	IsFamilyFriendly *bool        `json:"isFamilyFriendly"`
	Value            []ImageValue `json:"value"`
}

// ImageValue represents a single image result.
type ImageValue struct {
	WebSearchURL       *string    `json:"webSearchUrl"`
	Name               *string    `json:"name"`
	ThumbnailURL       string     `json:"thumbnailUrl"`
	DatePublished      *string    `json:"datePublished"`
	ContentURL         string     `json:"contentUrl"`
	HostPageURL        string     `json:"hostPageUrl"`
	ContentSize        *string    `json:"contentSize"`
	EncodingFormat     *string    `json:"encodingFormat"`
	HostPageDisplayURL string     `json:"hostPageDisplayUrl"`
	Width              int        `json:"width"`
	Height             int        `json:"height"`
	Thumbnail          *Thumbnail `json:"thumbnail"`
	IsFamilyFriendly   *bool      `json:"isFamilyFriendly"`
}

// ---------------------------------------------------------------------------
// Videos
// ---------------------------------------------------------------------------

// WebSearchVideos contains the video search results.
type WebSearchVideos struct {
	ID               string       `json:"id"`
	ReadLink         string       `json:"readLink"`
	WebSearchURL     string       `json:"webSearchUrl"`
	IsFamilyFriendly *bool        `json:"isFamilyFriendly"`
	Scenario         string       `json:"scenario"`
	Value            []VideoValue `json:"value"`
}

// VideoValue represents a single video result.
type VideoValue struct {
	WebSearchURL       string      `json:"webSearchUrl"`
	Name               string      `json:"name"`
	Description        string      `json:"description"`
	ThumbnailURL       string      `json:"thumbnailUrl"`
	Publisher          []Publisher `json:"publisher"`
	Creator            *Creator    `json:"creator"`
	ContentURL         string      `json:"contentUrl"`
	HostPageURL        string      `json:"hostPageUrl"`
	EncodingFormat     string      `json:"encodingFormat"`
	HostPageDisplayURL string      `json:"hostPageDisplayUrl"`
	Width              int         `json:"width"`
	Height             int         `json:"height"`
	Duration           string      `json:"duration"`
	MotionThumbnailURL string      `json:"motionThumbnailUrl"`
	EmbedHTML          string      `json:"embedHtml"`
	AllowHttpsEmbed    bool        `json:"allowHttpsEmbed"`
	ViewCount          int         `json:"viewCount"`
	Thumbnail          *Thumbnail  `json:"thumbnail"`
	AllowMobileEmbed   bool        `json:"allowMobileEmbed"`
	IsSuperfresh       bool        `json:"isSuperfresh"`
	DatePublished      string      `json:"datePublished"`
	IsFamilyFriendly   *bool       `json:"isFamilyFriendly"`
}

// Creator represents the creator of a video.
type Creator struct {
	Name string `json:"name"`
}

// Publisher represents the publisher of a video.
type Publisher struct {
	Name string `json:"name"`
}

// Thumbnail represents thumbnail dimensions.
type Thumbnail struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

// ---------------------------------------------------------------------------
// FormatSearchResultToLLMText converts a WebSearchResponse into an
// LLM-friendly input text.
//
// Parameters:
//   - resp:        the search response
//   - maxWebPages: max number of WebPageValue entries to include
//   - maxImages:   max number of ImageValue entries to include
//   - maxVideos:   max number of VideoValue entries to include
//   - maxRuneLen:  max rune length of the output text; truncated with "..."
//
// Returns the formatted text, measured in runes.
// ---------------------------------------------------------------------------
func FormatSearchResultToLLMText(resp *WebSearchResponse, maxWebPages, maxImages, maxVideos, maxRuneLen int) string {
	if resp == nil || resp.Data == nil {
		return ""
	}

	var sb strings.Builder

	// --- Web Pages ---
	if resp.Data.WebPages != nil && len(resp.Data.WebPages.Value) > 0 && maxWebPages > 0 {
		sb.WriteString("[Web Pages]\n")
		n := maxWebPages
		if n > len(resp.Data.WebPages.Value) {
			n = len(resp.Data.WebPages.Value)
		}
		for i, page := range resp.Data.WebPages.Value[:n] {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, page.Name))
			sb.WriteString(fmt.Sprintf("   URL: %s\n", page.URL))
			if page.Snippet != "" {
				sb.WriteString(fmt.Sprintf("   Snippet: %s\n", page.Snippet))
			}
			if page.Summary != "" {
				sb.WriteString(fmt.Sprintf("   Summary: %s\n", page.Summary))
			}
			sb.WriteString("\n")
		}
	}

	// --- Images ---
	if resp.Data.Images != nil && len(resp.Data.Images.Value) > 0 && maxImages > 0 {
		sb.WriteString("[Images]\n")
		n := maxImages
		if n > len(resp.Data.Images.Value) {
			n = len(resp.Data.Images.Value)
		}
		for i, img := range resp.Data.Images.Value[:n] {
			name := ""
			if img.Name != nil {
				name = *img.Name
			}
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, name))
			sb.WriteString(fmt.Sprintf("   URL: %s\n", img.ContentURL))
			sb.WriteString("\n")
		}
	}

	// --- Videos ---
	if resp.Data.Videos != nil && len(resp.Data.Videos.Value) > 0 && maxVideos > 0 {
		sb.WriteString("[Videos]\n")
		n := maxVideos
		if n > len(resp.Data.Videos.Value) {
			n = len(resp.Data.Videos.Value)
		}
		for i, video := range resp.Data.Videos.Value[:n] {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, video.Name))
			sb.WriteString(fmt.Sprintf("   URL: %s\n", video.ContentURL))
			if video.Description != "" {
				sb.WriteString(fmt.Sprintf("   Description: %s\n", video.Description))
			}
			sb.WriteString("\n")
		}
	}

	// Truncate by rune length
	result := sb.String()
	runes := []rune(result)
	if len(runes) > maxRuneLen {
		runes = runes[:maxRuneLen]
		// Ensure it ends with "..."
		result = string(runes) + "..."
	}

	return result
}
