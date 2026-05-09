package toolcalls

import (
	"BrainOnline/infra/i18n"
	"BrainOnline/infra/llm"
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// ============================================================
// Web Search — online search interface
// ============================================================

// WebSource represents a web search result source.
// Used for online search results with page URL.
type WebSource struct {
	Title       string  `json:"title"`
	Content     string  `json:"content,omitempty"`
	URL         string  `json:"url,omitempty"`          // Web page URL
	SiteName    string  `json:"site_name,omitempty"`    // Website name (e.g. "知乎", "CSDN")
	SiteIcon    string  `json:"site_icon,omitempty"`    // Website favicon URL
	PublishDate string  `json:"publish_date,omitempty"` // Page publish date, formatted string e.g. "2006-01-02"
	Score       float64 `json:"score"`
}

// WebSearcher is the web search interface (decoupled for testability).
// SearchForLLM performs a web search and returns an LLM-friendly formatted text
// along with the raw web page results for frontend display.
type WebSearcher interface {
	SearchForLLM(ctx context.Context, query string, freshness string, count int) (llmText string, webPages []WebSource, err error)
}

// ============================================================
// Web Search Tool — LLM function-calling for online search
// ============================================================

// WebSearchToolName is the name of the tool used for web search.
// The LLM can call this tool when it determines that online search is needed.
const WebSearchToolName = "web_search"

// SearchQueriesFromToolCall parses the search query from a tool call's arguments.
// arguments is the JSON string from the tool call's Function.Arguments field.
func SearchQueriesFromToolCall(id, arguments string) (string, error) {
	var result struct {
		SearchQueries string `json:"search_queries"`
	}
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return "", fmt.Errorf("failed to parse search query from tool call arguments. call id %s. %w", id, err)
	}
	return result.SearchQueries, nil
}

// ExecuteWebSearch performs the actual web search and returns the results.
func ExecuteWebSearch(ctx context.Context, searcher WebSearcher, query string) (searchResultText string, webPages []WebSource, err error) {
	if searcher == nil {
		return "", nil, fmt.Errorf("web search client not configured")
	}
	if query == "" {
		return "", nil, nil
	}
	searchResultText, webPages, err = searcher.SearchForLLM(ctx, query, "", 10)
	if err != nil {
		log.Printf("Web search failed: %v", err)
		return "", nil, err
	}
	return
}

// WebSearchToolDefinition returns the ToolDefinition for web search
// using llm types, with translated descriptions.
func WebSearchToolDefinition(lang string) llm.ToolDefinition {
	// Build the schema as a Go map and marshal it to JSON.
	// Using json.Marshal ensures the description string is properly escaped
	// (e.g., double quotes, newlines, etc.), so any translation content is safe.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"search_queries": map[string]any{
				"type":        "string",
				"description": i18n.TL(lang, "web_search_param_description"),
			},
		},
		"required":             []string{"search_queries"},
		"additionalProperties": false,
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal web search tool schema: %v", err))
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(schemaBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to parse web search tool schema: %v", err))
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        WebSearchToolName,
			Description: i18n.TL(lang, "web_search_tool_description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}
