package agent

import (
	"context"
	"fmt"
	"log"

	"BrainOnline/infra/3rdapi/llm_raw"
	"BrainOnline/infra/sse"
)

// ============================================================
// ToolExecutor implementation — ChatHandler executes tools for DeepSeekRaw
// ============================================================

// ExecuteTool implements llm_raw.ToolExecutor.
// It dispatches tool calls by name to the appropriate handler.
func (h *ChatHandler) ExecuteTool(ctx context.Context, toolName string, toolCallID string, arguments string) (string, error) {
	switch toolName {
	case webSearchToolName:
		return h.executeWebSearchTool(ctx, toolCallID, arguments)
	default:
		log.Printf("Unknown tool call: %s (only web_search is supported)", toolName)
		return fmt.Sprintf("Unknown tool '%s' — no handler available", toolName), nil
	}
}

// executeWebSearchTool parses the search query and executes a web search.
func (h *ChatHandler) executeWebSearchTool(ctx context.Context, toolCallID string, arguments string) (string, error) {
	query, parseErr := searchQueriesFromToolCall(toolCallID, arguments)
	if parseErr != nil {
		return fmt.Sprintf("Failed to parse search query: %v", parseErr), nil
	}

	searchResultText, _, searchErr := h.executeWebSearch(ctx, query)
	if searchErr != nil {
		log.Printf("Web search failed: %v", searchErr)
	}

	if searchResultText == "" {
		return "Search returned no results", nil
	}
	return searchResultText, nil
}

// ============================================================
// SSEStreamCallback — adapts StreamCallback to SSE writer
// ============================================================

// sseStreamCallback implements llm_raw.StreamCallback by writing events
// to an SSE writer for the frontend.
type sseStreamCallback struct {
	sseWriter *sse.SSEWriter
	webPages  *[]WebSource
}

func newSSEStreamCallback(sseWriter *sse.SSEWriter, webPages *[]WebSource) *sseStreamCallback {
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

func (cb *sseStreamCallback) OnToolCallStart(ctx context.Context, toolName string, arguments string) error {
	// For web_search, send a "web_search" SSE event to notify the frontend
	if toolName == webSearchToolName {
		query, parseErr := searchQueriesFromToolCall("", arguments)
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
func (h *ChatHandler) performLLMStreamingCall(
	ctx context.Context,
	sseWriter *sse.SSEWriter,
	messages []llm_raw.Message,
	tools []llm_raw.ToolDefinition,
) (fullReply string, webPages []WebSource, err error) {

	// Create the SSE callback
	var pages []WebSource
	callback := newSSEStreamCallback(sseWriter, &pages)

	// Delegate to DeepSeekRaw
	reply, err := h.llmClient.PerformLLMStreamingCall(ctx, callback, messages, tools, h)
	if err != nil {
		return "", pages, err
	}

	return reply, pages, nil
}

// ============================================================
// LLM Streaming + Thinking Call — delegates to DeepSeekRaw
// ============================================================

// performLLMStreamingThinkingCall performs a streaming LLM call in deep-thinking
// mode. It delegates to DeepSeekRaw.PerformLLMStreamingThinkingCall.
func (h *ChatHandler) performLLMStreamingThinkingCall(
	ctx context.Context,
	sseWriter *sse.SSEWriter,
	messages []llm_raw.Message,
	tools []llm_raw.ToolDefinition,
) (fullReply string, webPages []WebSource, err error) {

	// Create the SSE callback
	var pages []WebSource
	callback := newSSEStreamCallback(sseWriter, &pages)

	// Delegate to DeepSeekRaw in thinking mode
	reply, err := h.llmClient.PerformLLMStreamingThinkingCall(ctx, callback, messages, tools, h)
	if err != nil {
		return "", pages, err
	}

	return reply, pages, nil
}
