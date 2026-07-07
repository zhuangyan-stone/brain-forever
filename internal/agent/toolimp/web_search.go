package toolimp

import (
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/llmtypes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ============================================================
// Web Search -online search interface
// ============================================================

// WebSource represents a web search result source.
// Re-exported from llmtypes for backward compatibility.
type WebSource = llmtypes.WebSource

// WebSearcher is the web search interface (decoupled for testability).
// SearchForLLM performs a web search and returns an LLM-friendly formatted text
// along with the raw web page results for frontend display.
type WebSearcher interface {
	SearchForLLM(ctx context.Context, query string, freshness string, count int) (llmText string, webPages []WebSource, err error)
}

// ============================================================
// Web Search Tool -LLM function-calling for online search
// ============================================================

// WebSearchToolName is the name of the tool used for web search.
// The LLM can call this tool when it determines that online search is needed.
const WebSearchToolName = "web_search"

func webSearchArguments(arguments string) (string, error) {
	var result struct {
		SearchQueries string `json:"search_queries"`
	}
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return "", fmt.Errorf("unmarshal arguments: %w", err)
	}
	return result.SearchQueries, nil
}

// executeWebSearch performs the actual web search and returns the results.
// Caller must ensure searcher is not nil.
func executeWebSearch(ctx context.Context, searcher WebSearcher, query string) (searchResultText string, webPages []WebSource, err error) {
	if query == "" {
		return "", nil, nil
	}
	searchResultText, webPages, err = searcher.SearchForLLM(ctx, query, "", 10)
	if err != nil {
		return "", nil, err
	}
	return
}

// webSearchToolDefinition returns the ToolDefinition for web search
// using llm types, with translated descriptions.
func webSearchToolDefinition(lang string) llm.ToolDefinition {
	// Build the schema as a Go map and marshal it to JSON.
	// Using json.Marshal ensures the description string is properly escaped
	// (e.g., double quotes, newlines, etc.), so any translation content is safe.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"search_queries": map[string]any{
				"type":        "string",
				"description": i18n.Tools.TL(lang, WebSearchToolName, "param_description"),
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
			Description: i18n.Tools.TL(lang, WebSearchToolName, "description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}

// WebSearchToolImp
type WebSearchToolImp struct {
	ctx context.Context

	def llm.ToolDefinition

	searcher WebSearcher
	lang     string

	q string

	// result
	WebPages []WebSource
}

var _ llm.ToolIMP = (*WebSearchToolImp)(nil)

func MakeWebSearchTool(ctx context.Context, searcher WebSearcher, lang string) *WebSearchToolImp {
	return &WebSearchToolImp{
		ctx:      ctx,
		def:      webSearchToolDefinition(lang),
		searcher: searcher,
		lang:     lang,
	}
}

func (imp *WebSearchToolImp) GetName() string {
	return WebSearchToolName
}

func (imp *WebSearchToolImp) GetDefinition() llm.ToolDefinition {
	return imp.def
}

func (imp *WebSearchToolImp) GetPendingText() string {
	return fmt.Sprintf("%s %s", i18n.Tools.TL(imp.lang, WebSearchToolName, "pending"), imp.q)
}

func (imp *WebSearchToolImp) SetArgument(arguments string) (err error) {
	imp.q, err = webSearchArguments(arguments)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.TL(imp.lang, "web_search_error_unmarshal_args", nil), err)
	}
	return
}

func (imp *WebSearchToolImp) Execute() (result string, err error) {
	if imp.q == "" {
		return "", errors.New(i18n.TL(imp.lang, "web_search_error_empty_query", nil))
	}
	if imp.searcher == nil {
		return "", errors.New(i18n.TL(imp.lang, "web_search_error_searcher_not_init", nil))
	}

	// Execute the web search
	var pages []WebSource
	result, pages, err = executeWebSearch(imp.ctx, imp.searcher, imp.q)
	if err != nil {
		return "", err
	}

	// Accumulate results across multiple tool calls in the same thinking process
	imp.WebPages = append(imp.WebPages, pages...)
	return
}
