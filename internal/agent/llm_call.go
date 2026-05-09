package agent

import (
	"context"
	"fmt"
	"log"

	"BrainOnline/infra/httpx/sse"
	"BrainOnline/infra/i18n"
	"BrainOnline/infra/llm"
	"BrainOnline/internal/agent/toolcalls"
)

// ============================================================
// ToolExecutor implementation — ChatHandler executes tools for DeepSeekRaw
// ============================================================

// ExecuteTool implements llm.ToolExecutor.
// It dispatches tool calls by name to the appropriate handler.
// The returned messages are translated according to the handler's default language.
func (h *ChatAgent) ExecuteTool(ctx context.Context, toolName string, toolCallID string, arguments string) (string, error) {
	switch toolName {
	case toolcalls.WebSearchToolName:
		return h.executeWebSearchTool(ctx, toolCallID, arguments)
	case toolcalls.TimeQueryToolName:
		return h.executeTimeQueryTool(ctx, toolCallID, arguments)
	default:
		log.Printf("Unknown tool call: %s (only web_search and get_current_local_time are supported)", toolName)
		return i18n.TL(h.defaultLang, "unknown_tool", map[string]interface{}{
			"ToolName": toolName,
		}), nil
	}
}

// executeTimeQueryTool executes the get_current_local_time tool.
// This tool takes no arguments — it simply returns the current local time
// with timezone information.
func (h *ChatAgent) executeTimeQueryTool(ctx context.Context, toolCallID string, arguments string) (string, error) {
	return toolcalls.ExecuteTimeQuery(), nil
}

// executeWebSearchTool parses the search query and executes a web search.
// The returned messages are translated according to the handler's default language.
func (h *ChatAgent) executeWebSearchTool(ctx context.Context, toolCallID string, arguments string) (string, error) {
	query, parseErr := toolcalls.SearchQueriesFromToolCall(toolCallID, arguments)
	if parseErr != nil {
		return i18n.TL(h.defaultLang, "search_parse_error", map[string]interface{}{
			"Error": parseErr,
		}), nil
	}

	searchResultText, webPages, searchErr := toolcalls.ExecuteWebSearch(ctx, h.webSearcher, query)
	if searchErr != nil {
		log.Printf("Web search failed: %v", searchErr)
	}

	// Store web pages into the collector so they can be sent to the frontend
	// as a "sources" SSE event after the streaming call completes.
	// Deduplicate by URL to avoid sending duplicate references to the frontend
	// when the same page appears across multiple search rounds.
	if len(webPages) > 0 && h.webPagesCollector != nil {
		// Build a set of existing URLs for O(1) lookup
		existing := make(map[string]bool, len(*h.webPagesCollector))
		for _, p := range *h.webPagesCollector {
			if p.URL != "" {
				existing[p.URL] = true
			}
		}
		// Only append pages whose URL is not already in the collector
		for _, p := range webPages {
			if p.URL == "" || !existing[p.URL] {
				*h.webPagesCollector = append(*h.webPagesCollector, p)
				if p.URL != "" {
					existing[p.URL] = true
				}
			}
		}
	}

	if searchResultText == "" {
		return i18n.TL(h.defaultLang, "search_no_results"), nil
	}
	return searchResultText, nil
}

// ============================================================
// SSEStreamCallback — adapts StreamCallback to SSE writer
// ============================================================

// sseStreamCallback implements llm.StreamCallback by writing events
// to an SSE writer for the frontend.
type sseStreamCallback struct {
	sseWriter *sse.Writer
	webPages  *[]toolcalls.WebSource
}

func newSSEStreamCallback(sseWriter *sse.Writer, webPages *[]toolcalls.WebSource) *sseStreamCallback {
	return &sseStreamCallback{
		sseWriter: sseWriter,
		webPages:  webPages,
	}
}

func (cb *sseStreamCallback) OnText(ctx context.Context, delta string) error {
	return cb.sseWriter.WriteEvent(SSEEvent{
		Type:    "text",
		Content: delta,
	})
}

func (cb *sseStreamCallback) OnReasoning(ctx context.Context, delta string) error {
	return cb.sseWriter.WriteEvent(SSEEvent{
		Type:    "reasoning",
		Content: delta,
	})
}

func (cb *sseStreamCallback) OnToolCallStart(ctx context.Context, toolName string, toolCallID string, arguments string) error {
	// For web_search, send a "web_search" SSE event to notify the frontend
	if toolName == toolcalls.WebSearchToolName {
		query, parseErr := toolcalls.SearchQueriesFromToolCall(toolCallID, arguments)
		if parseErr == nil && query != "" {
			return cb.sseWriter.WriteEvent(SSEEvent{
				Type:    "web_search",
				Content: query,
			})
		}
	}
	return nil
}

func (cb *sseStreamCallback) OnToolCallResult(ctx context.Context, toolName string, toolCallID string, result string) error {
	// No SSE event needed for tool results currently
	return nil
}

func (cb *sseStreamCallback) OnError(ctx context.Context, err error) error {
	return cb.sseWriter.WriteEvent(SSEEvent{
		Type:    "error",
		Message: fmt.Sprintf("%v", err),
	})
}

// ============================================================
// LLM Streaming Call — delegates to DeepSeekRaw
// ============================================================

// performLLMStreamingCall performs a streaming LLM call with tool support.
// It delegates to DeepSeekRaw.PerformLLMStreamingCall, which handles the
// tool call loop internally.
func (h *ChatAgent) performLLMStreamingCall(
	ctx context.Context,
	sseWriter *sse.Writer,
	messages []llm.Message,
	tools []llm.ToolDefinition,
) (fullReply string, webPages []toolcalls.WebSource, err error) {

	// Create the SSE callback
	var pages []toolcalls.WebSource
	callback := newSSEStreamCallback(sseWriter, &pages)

	// Set the web pages collector so executeWebSearchTool can store results
	h.webPagesCollector = &pages

	// Delegate to DeepSeekRaw
	reply, err := h.charLLMClient.PerformLLMStreamingCall(ctx, callback, messages, tools, h)

	// Clear the collector
	h.webPagesCollector = nil

	if err != nil {
		return "", pages, err
	}

	return reply, pages, nil
}
