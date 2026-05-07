package agent

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"BrainOnline/infra/3rdapi/llm"
	"BrainOnline/infra/sse"
)

// ============================================================
// LLM Streaming Call — generic streaming loop with tool support
// ============================================================

// streamingToolCall stores tool call data collected from streaming deltas.
// We use a custom struct because ChatCompletionMessageToolCall (response type)
// does not have an Index field, but we need it to match streaming deltas.
type streamingToolCall struct {
	Index     int64
	ID        string
	Type      string
	Name      string
	Arguments string
}

// toParam converts a streamingToolCall to a ChatCompletionMessageToolCallUnionParam
// for use in assistant message construction.
func (stc *streamingToolCall) toParam() openai.ChatCompletionMessageToolCallUnionParam {
	return openai.ChatCompletionMessageToolCallUnionParam{
		OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
			ID: stc.ID,
			// Type defaults to "function" — no need to set it explicitly
			Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
				Name:      stc.Name,
				Arguments: stc.Arguments,
			},
		},
	}
}

// maxToolCallIterations is the maximum number of tool call iterations
// to prevent infinite loops when the LLM repeatedly calls tools.
const maxToolCallIterations = 5

// performLLMStreamingCall performs a streaming LLM call with tool support.
// If the LLM calls a tool (e.g. web_search), it executes the tool, sends the
// corresponding SSE event, appends the tool result, and re-streams with the
// updated messages.
// Returns the final assistant reply content and any web pages found.
func (h *ChatHandler) performLLMStreamingCall(
	ctx context.Context,
	sseWriter *sse.SSEWriter,
	messages []openai.ChatCompletionMessageParamUnion,
	tools []openai.ChatCompletionToolUnionParam,
) (fullReply string, webPages []WebSource, err error) {

	toolCallIterations := 0

	for {
		// Safety check: prevent infinite tool call loops
		toolCallIterations++
		if toolCallIterations > maxToolCallIterations {
			return "", webPages, fmt.Errorf("tool call iteration limit (%d) exceeded — possible infinite loop", maxToolCallIterations)
		}

		// Call streaming API with the available tools
		stream := h.aiClient.ChatStreamWithOptions(ctx, openai.ChatCompletionNewParams{
			Messages: messages,
			Tools:    tools,
			// No ToolChoice — let the LLM decide whether to call the tool
		})
		if stream == nil {
			return "", nil, fmt.Errorf("failed to call LLM API: client not initialized")
		}

		// Collect the full assistant response (text + reasoning + tool calls)
		var replyBuilder strings.Builder
		var reasoningBuilder strings.Builder
		var collectedToolCalls []streamingToolCall
		finishReason := ""

		for stream.Next() {
			chunk := stream.Current()

			// Extract token usage from the final chunk (only present when
			// stream_options.include_usage is true, which is set in ChatStreamWithOptions).
			// The usage chunk has empty choices, so we check Usage.TotalTokens > 0.
			if chunk.Usage.TotalTokens > 0 {
				h.aiClient.SetUsageInfo(chunk.Usage)
			}

			for _, choice := range chunk.Choices {
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}

				// Collect tool call deltas (streaming tool calls come in chunks)
				for _, tc := range choice.Delta.ToolCalls {
					// Find or create the tool call by index
					found := false
					for i := range collectedToolCalls {
						if collectedToolCalls[i].Index == tc.Index {
							// Append to existing tool call
							if tc.Function.Name != "" {
								collectedToolCalls[i].Name = tc.Function.Name
							}
							if tc.Function.Arguments != "" {
								collectedToolCalls[i].Arguments += tc.Function.Arguments
							}
							if tc.ID != "" {
								collectedToolCalls[i].ID = tc.ID
							}
							if tc.Type != "" {
								collectedToolCalls[i].Type = tc.Type
							}
							found = true
							break
						}
					}
					if !found {
						// New tool call
						collectedToolCalls = append(collectedToolCalls, streamingToolCall{
							Index:     tc.Index,
							ID:        tc.ID,
							Type:      tc.Type,
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						})
					}
				}

				// Extract reasoning_content from the chunk's raw JSON.
				// DeepSeek v4-flash may include reasoning_content even without
				// explicit thinking mode enabled, and the API requires it to be
				// passed back in subsequent assistant messages when tool_calls
				// are present.
				reasoningContent := llm.GetReasoningContent(chunk)
				if reasoningContent != "" {
					reasoningBuilder.WriteString(reasoningContent)
				}

				// Forward text content to frontend
				if choice.Delta.Content != "" {
					if err := sseWriter.WriteEvent(SSEEvent{
						Type:    "text",
						Content: choice.Delta.Content,
					}); err != nil {
						return "", nil, fmt.Errorf("failed to write text event. %w", err)
					}
					replyBuilder.WriteString(choice.Delta.Content)
				}
			}
		}

		if err := stream.Err(); err != nil {
			return "", nil, fmt.Errorf("stream error. %w", err)
		}

		// Check if the LLM decided to call a tool
		if finishReason == "tool_calls" && len(collectedToolCalls) > 0 {
			// Build the assistant message with tool calls (for history)
			toolCallParams := make([]openai.ChatCompletionMessageToolCallUnionParam, len(collectedToolCalls))
			for i, tc := range collectedToolCalls {
				toolCallParams[i] = tc.toParam()
			}

			// When tool_calls are present, content should be null (not empty string)
			// to comply with the OpenAI/DeepSeek API spec. An empty string can cause
			// parsing issues in subsequent requests.
			assistantMsg := openai.ChatCompletionMessageParamUnion{
				OfAssistant: &openai.ChatCompletionAssistantMessageParam{
					ToolCalls: toolCallParams,
				},
			}
			// Only set content if there is actual text content
			if replyBuilder.Len() > 0 {
				assistantMsg.OfAssistant.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(replyBuilder.String()),
				}
			}

			// DeepSeek models (including v4-flash) require reasoning_content to
			// be present in the assistant message when tool_calls are included.
			// Since ChatCompletionAssistantMessageParam does not have a built-in
			// ReasoningContent field, we use SetExtraFields to inject it into
			// the JSON serialization via param.MarshalWithExtras.
			if reasoningBuilder.Len() > 0 {
				assistantMsg.OfAssistant.SetExtraFields(map[string]any{
					"reasoning_content": reasoningBuilder.String(),
				})
			}

			// Append the assistant message first, then tool result messages for each tool call.
			// This ensures every tool_call_id in the assistant message has a corresponding
			// tool response, which is required by the OpenAI/DeepSeek API spec.
			messages = append(messages, assistantMsg)

			for _, tc := range collectedToolCalls {
				switch tc.Name {
				case webSearchToolName:
					// Parse search query from the tool call
					query, parseErr := searchQueriesFromToolCall(tc.ID, tc.Arguments)
					if parseErr != nil {
						log.Printf("Failed to parse search query: %v", parseErr)
						// Still add a tool result message to satisfy the API requirement
						messages = append(messages, openai.ToolMessage("Failed to parse search query", tc.ID))
						continue
					}

					// Send "web_search" SSE event to notify the frontend
					if err := sseWriter.WriteEvent(SSEEvent{
						Type:    "web_search",
						Content: query,
					}); err != nil {
						log.Printf("failed to write web_search event: %v", err)
					}

					// Execute the web search
					searchResultText, pages, searchErr := h.executeWebSearch(ctx, query)
					if searchErr != nil {
						log.Printf("Web search failed: %v", searchErr)
					}
					if pages != nil {
						webPages = pages
					}

					// Build the tool result message
					toolResultContent := "Search returned no results"
					if searchResultText != "" {
						toolResultContent = searchResultText
					}
					messages = append(messages, openai.ToolMessage(toolResultContent, tc.ID))

				default:
					// Unknown tool call — provide a fallback tool result so the API
					// doesn't reject the request due to missing tool responses.
					log.Printf("Unknown tool call: %s (only web_search is supported)", tc.Name)
					messages = append(messages, openai.ToolMessage(
						fmt.Sprintf("Unknown tool '%s' — no handler available", tc.Name), tc.ID))
				}
			}

			// Close current stream before looping
			stream.Close()

			// Continue the loop to re-stream with the tool result
			continue
		}

		// Normal completion (stop, length, etc.) — return the reply
		return replyBuilder.String(), webPages, nil
	}
}

// ============================================================
// LLM Streaming + Thinking Call — deep-thinking mode with tool support
// ============================================================

// performLLMStreamingThinkingCall performs a streaming LLM call in deep-thinking
// mode (deepseek-v4-pro). It streams reasoning_content to the frontend as SSE
// "reasoning" events, and supports multiple tool calls across multiple sub-turns.
//
// The function follows the DeepSeek thinking mode protocol:
//   - reasoning_content and content can appear in the same or different chunks
//   - assistant messages always include reasoning_content (API ignores it when
//     there were no tool calls in that turn)
//   - tool results trigger re-entry into the thinking loop
//
// Returns the final assistant reply content and any web pages found.
func (h *ChatHandler) performLLMStreamingThinkingCall(
	ctx context.Context,
	sseWriter *sse.SSEWriter,
	messages []openai.ChatCompletionMessageParamUnion,
	tools []openai.ChatCompletionToolUnionParam,
) (fullReply string, webPages []WebSource, err error) {

	toolCallIterations := 0

	for {
		// Safety check: prevent infinite tool call loops
		toolCallIterations++
		if toolCallIterations > maxToolCallIterations {
			return "", webPages, fmt.Errorf("tool call iteration limit (%d) exceeded in thinking mode — possible infinite loop", maxToolCallIterations)
		}

		// Build base streaming options for thinking mode
		streamOpts := []option.RequestOption{
			option.WithJSONSet("thinking", map[string]any{"type": "enabled"}),
		}

		// Call streaming API in thinking mode
		stream := h.aiClient.ChatStreamWithOptions(ctx, openai.ChatCompletionNewParams{
			Messages:        messages,
			Tools:           tools,
			ReasoningEffort: shared.ReasoningEffortHigh,
			// No ToolChoice — let the LLM decide whether to call a tool
		},
			streamOpts...,
		)
		if stream == nil {
			return "", nil, fmt.Errorf("failed to call LLM API (thinking mode): client not initialized")
		}

		// Collect the full assistant response (reasoning + content + tool calls)
		var reasoningBuilder strings.Builder
		var replyBuilder strings.Builder
		var collectedToolCalls []streamingToolCall
		finishReason := ""

		for stream.Next() {
			chunk := stream.Current()

			// Extract token usage from the final chunk (only present when
			// stream_options.include_usage is true, which is set in ChatStreamWithOptions).
			// The usage chunk has empty choices, so we check Usage.TotalTokens > 0.
			if chunk.Usage.TotalTokens > 0 {
				h.aiClient.SetUsageInfo(chunk.Usage)
			}

			for _, choice := range chunk.Choices {
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}

				// Collect tool call deltas (streaming tool calls come in chunks)
				for _, tc := range choice.Delta.ToolCalls {
					// Find or create the tool call by index
					found := false
					for i := range collectedToolCalls {
						if collectedToolCalls[i].Index == tc.Index {
							// Append to existing tool call
							if tc.Function.Name != "" {
								collectedToolCalls[i].Name = tc.Function.Name
							}
							if tc.Function.Arguments != "" {
								collectedToolCalls[i].Arguments += tc.Function.Arguments
							}
							if tc.ID != "" {
								collectedToolCalls[i].ID = tc.ID
							}
							if tc.Type != "" {
								collectedToolCalls[i].Type = tc.Type
							}
							found = true
							break
						}
					}
					if !found {
						// New tool call
						collectedToolCalls = append(collectedToolCalls, streamingToolCall{
							Index:     tc.Index,
							ID:        tc.ID,
							Type:      tc.Type,
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						})
					}
				}

				// Extract reasoning_content from the chunk's raw JSON
				reasoningContent := llm.GetReasoningContent(chunk)

				// Forward reasoning content to frontend (thinking process)
				if reasoningContent != "" {
					if err := sseWriter.WriteEvent(SSEEvent{
						Type:    "reasoning",
						Content: reasoningContent,
					}); err != nil {
						return "", nil, fmt.Errorf("failed to write reasoning event. %w", err)
					}
					reasoningBuilder.WriteString(reasoningContent)
				}

				// Forward text content to frontend
				if choice.Delta.Content != "" {
					if err := sseWriter.WriteEvent(SSEEvent{
						Type:    "text",
						Content: choice.Delta.Content,
					}); err != nil {
						return "", nil, fmt.Errorf("failed to write text event. %w", err)
					}
					replyBuilder.WriteString(choice.Delta.Content)
				}
			}
		}

		if err := stream.Err(); err != nil {
			return "", nil, fmt.Errorf("stream error (thinking mode). %w", err)
		}

		// Check if the LLM decided to call a tool
		if finishReason == "tool_calls" && len(collectedToolCalls) > 0 {
			// Build the assistant message with tool_calls.
			// This mirrors the Python equivalent:
			//   messages.append(response.choices[0].message)
			// which includes content, reasoning_content, and tool_calls.

			// Convert streamingToolCall to ChatCompletionMessageToolCallUnionParam
			toolCallParams := make([]openai.ChatCompletionMessageToolCallUnionParam, len(collectedToolCalls))
			for i, tc := range collectedToolCalls {
				toolCallParams[i] = tc.toParam()
			}

			// When tool_calls are present, content should be null (not empty string)
			// to comply with the OpenAI/DeepSeek API spec. An empty string can cause
			// parsing issues in subsequent requests.
			assistantMsg := openai.ChatCompletionMessageParamUnion{
				OfAssistant: &openai.ChatCompletionAssistantMessageParam{
					ToolCalls: toolCallParams,
				},
			}
			// Only set content if there is actual text content
			if replyBuilder.Len() > 0 {
				assistantMsg.OfAssistant.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(replyBuilder.String()),
				}
			}

			// DeepSeek thinking mode requires reasoning_content to be present
			// in the assistant message when tool_calls are included. Since
			// ChatCompletionAssistantMessageParam does not have a built-in
			// ReasoningContent field, we use SetExtraFields to inject it into
			// the JSON serialization via param.MarshalWithExtras.
			if reasoningBuilder.Len() > 0 {
				assistantMsg.OfAssistant.SetExtraFields(map[string]any{
					"reasoning_content": reasoningBuilder.String(),
				})
			}

			// Execute each tool call and collect results.
			// The assistant message is appended once, followed by all tool result messages.
			// This matches the Python pattern:
			//   for tool in tool_calls:
			//       messages.append({"role": "tool", ...})
			// where the assistant message is appended before the loop.
			messages = append(messages, assistantMsg)

			for _, tc := range collectedToolCalls {
				switch tc.Name {
				case webSearchToolName:
					// Parse search query
					query, parseErr := searchQueriesFromToolCall(tc.ID, tc.Arguments)
					if parseErr != nil {
						log.Printf("Failed to parse search query: %v", parseErr)
						continue
					}

					// Send "web_search" SSE event to notify the frontend
					if err := sseWriter.WriteEvent(SSEEvent{
						Type:    "web_search",
						Content: query,
					}); err != nil {
						log.Printf("failed to write web_search event: %v", err)
					}

					// Execute the web search
					searchResultText, pages, searchErr := h.executeWebSearch(ctx, query)
					if searchErr != nil {
						log.Printf("Web search failed: %v", searchErr)
					}
					if pages != nil {
						webPages = pages
					}

					// Build the tool result message
					toolResultContent := "Search returned no results"
					if searchResultText != "" {
						toolResultContent = searchResultText
					}
					toolResultMsg := openai.ToolMessage(toolResultContent, tc.ID)
					messages = append(messages, toolResultMsg)

				default:
					// Unknown tool call — this should not happen since only
					// web_search is registered, but log it defensively.
					log.Printf("Unknown tool call: %s (only web_search is supported)", tc.Name)
				}
			}

			// Close current stream before looping
			stream.Close()

			// Continue the loop to re-stream with the tool result
			continue
		}

		// Normal completion (stop, length, etc.) — return the reply
		return replyBuilder.String(), webPages, nil
	}
}
