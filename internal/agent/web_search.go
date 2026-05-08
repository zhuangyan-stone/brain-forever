package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

// ============================================================
// Web Search — online search interface
// ============================================================

// WebSearcher is the web search interface (decoupled for testability).
// SearchForLLM performs a web search and returns an LLM-friendly formatted text
// along with the raw web page results for frontend display.
type WebSearcher interface {
	SearchForLLM(ctx context.Context, query string, freshness string, count int) (llmText string, webPages []WebSource, err error)
}

// ============================================================
// Web Search Tool — LLM function-calling for online search
// ============================================================

// webSearchToolName is the name of the tool used for web search.
// The LLM can call this tool when it determines that online search is needed.
const webSearchToolName = "web_search"

// webSearchToolDefinition returns the Tool definition with strict JSON schema
// for the LLM to generate search queries.
func webSearchToolDefinition() openai.ChatCompletionToolUnionParam {
	const schema = `{
		"type": "object",
		"properties": {
			"search_queries": {
				"type": "string",
				"description": "搜索关键词"
			}
		},
		"required": ["search_queries"],
		"additionalProperties": false
	}`

	// Parse the JSON schema into FunctionParameters (map[string]any)
	var paramsMap map[string]any
	if err := json.Unmarshal([]byte(schema), &paramsMap); err != nil {
		// This should never happen since schema is a compile-time constant
		panic(fmt.Sprintf("failed to parse web search tool schema: %v", err))
	}

	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        webSearchToolName,
		Description: param.NewOpt("Call this tool to perform web searches when real-time information is needed (e.g., weather, news, stock prices, exchange rates, latest events, etc.). Generates a list of search keywords based on the user's input."),
		Strict:      param.NewOpt(true),
		Parameters:  shared.FunctionParameters(paramsMap),
	})
}

// searchQueriesFromToolCall parses the search query from a tool call's arguments.
// arguments is the JSON string from the tool call's Function.Arguments field.
func searchQueriesFromToolCall(id, arguments string) (string, error) {
	var result struct {
		SearchQueries string `json:"search_queries"`
	}
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return "", fmt.Errorf("failed to parse search query from tool call arguments. call id %s. %w", id, err)
	}
	return result.SearchQueries, nil
}

// executeWebSearch performs the actual web search and returns the results.
func (h *ChatHandler) executeWebSearch(ctx context.Context, query string) (searchResultText string, webPages []WebSource, err error) {
	if h.webSearcher == nil {
		return "", nil, fmt.Errorf("web search client not configured")
	}
	if query == "" {
		return "", nil, nil
	}
	searchResultText, webPages, err = h.webSearcher.SearchForLLM(ctx, query, "", 10)
	if err != nil {
		log.Printf("Web search failed: %v", err)
		return "", nil, err
	}
	return
}
