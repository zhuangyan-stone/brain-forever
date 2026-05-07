package searcher

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
// maxRuneLen controls the maximum rune length of the returned LLM text;
// when exceeded, the text is truncated with "...".
// The maximum number of web pages is taken from req.Count.
type WebSearcher interface {
	Search(ctx context.Context, req WebSearchRequest) (*WebSearchResponse, error)

	SearchForLLM(ctx context.Context, req WebSearchRequest, maxRuneLen int) (*WebSearchResponse, string, error)
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
	Query []string `json:"query"`

	// - noLimit
	// - oneDay
	// - oneWeek
	// - oneMonth
	// - oneYear
	// - YYYY-MM-DD..YYYY-MM-DD
	// - YYYY-MM-DD
	Freshness string `json:"freshness,omitempty"`

	Summary            bool   `json:"summary,omitempty"`
	Include            string `json:"include,omitempty"`
	Exclude            string `json:"exclude,omitempty"`
	Count              int    `json:"count,omitempty"`
	FamilyFriendlyOnly bool   `json:"family_friendly_only"`
}

// WebSearchResponse is the simplified search result.
type WebSearchResponse struct {
	Pages []WebPageValue
}

// WebPageValue represents a single web page result.
type WebPageValue struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	URL              string  `json:"url"`
	Snippet          string  `json:"snippet"`
	Summary          string  `json:"summary,omitempty"`
	SiteName         string  `json:"siteName"`
	SiteIcon         string  `json:"siteIcon"`
	Language         *string `json:"language"`
	PublishDate      string  `json:"publish_date"`
	IsFamilyFriendly *bool   `json:"isFamilyFriendly"`
}

// ---------------------------------------------------------------------------
// ResultToLLMText converts a WebSearchResponse into an LLM-friendly input text.
//
// Parameters:
//   - resp:        the search result to format
//   - maxRuneLen:  maximum rune length of the output text; truncated with "...".
//     Use -1 for unlimited length.
//
// Returns the formatted text.
// ---------------------------------------------------------------------------
func ResultToLLMText(resp *WebSearchResponse, maxRuneLen int) string {
	if resp == nil {
		return ""
	}

	var sb strings.Builder

	// --- Web Pages ---
	if len(resp.Pages) > 0 {
		sb.WriteString("[Web Pages]\n")
		for i, page := range resp.Pages {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, page.Name))
			sb.WriteString(fmt.Sprintf("   URL: %s\n", page.URL))
			if page.Snippet != "" {
				sb.WriteString(fmt.Sprintf("   Snippet: %s\n", page.Snippet))
			}
			if page.Summary != "" {
				sb.WriteString(fmt.Sprintf("   Summary: %s\n", page.Summary))
			}
			if page.SiteName != "" {
				sb.WriteString(fmt.Sprintf("   Site name: %s\n", page.Name))
			}
			if page.Language != nil && len(*page.Language) > 0 {
				sb.WriteString(fmt.Sprintf("   Site language: %s\n", page.SiteName))
			}

			if page.PublishDate != "" {
				sb.WriteString(fmt.Sprintf("   Date published: %s\n", page.PublishDate))
			}

			sb.WriteString("\n")
		}
	}

	// Truncate by rune length (skip if maxRuneLen is -1)
	result := sb.String()
	if maxRuneLen >= 0 {
		runes := []rune(result)
		if len(runes) > maxRuneLen {
			runes = runes[:maxRuneLen]
			// Ensure it ends with "..."
			result = string(runes) + "..."
		}
	}

	return result
}
