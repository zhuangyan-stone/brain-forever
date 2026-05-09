package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"BrainOnline/i18n"
	"BrainOnline/infra/httpx"
)

// ============================================================
// DeepSeekRaw — DeepSeek API client using raw http.Client
//
// This implementation uses net/http directly (no openai-go SDK dependency)
// to provide an alternative way of calling the DeepSeek API.
//
// It provides a high-level streaming method (PerformLLMStreamingCall) that
// handles tool call loops internally, delegating actual tool execution to a
// ToolExecutor interface.
//
// Usage:
/*
	client := NewDeepSeekRaw("", "DEEPSEEK_API_KEY", "deepseek-chat")
	resp, err := client.Chat(ctx, []Message{
		{Role: "user", Content: "Hello"},
	})

	stream := client.ChatStream(ctx, []Message{
		{Role: "user", Content: "Hello"},
	})
	for stream.Next() {
		chunk := stream.CurrentChatCompletionChunk()
	}
	if stream.Err() != nil { ... }

	result := client.PerformLLMStreamingCall(ctx, callback, messages, tools, executor)
*/
// ============================================================

// ============================================================
// DeepSeekRaw client
// ============================================================

// DeepSeekRaw is a DeepSeek API client that uses raw http.Client.
type DeepSeekRaw struct {
	apiKey                string
	baseURL               string
	model                 string
	thinkingEnabled       bool // true = enabled, false = disabled
	httpClient            *http.Client
	streamHTTPClient      *http.Client
	lastUsage             *Usage // token usage from the most recent API call
	maxToolCallIterations int    // max tool call iterations; 0 means default (5)
}

// NewDeepSeekRaw creates a new DeepSeekRaw client.
//
// apiKey: DeepSeek API Key, if empty reads from the env variable specified by envKey
// envKey: environment variable name, defaults to "DEEPSEEK_API_KEY"
// model:  model name (e.g. "deepseek-v4-flash", "deepseek-chat", "deepseek-reasoner")
func NewDeepSeekRaw(apiKey, envKey, model string, enableThinking bool) *DeepSeekRaw {
	if apiKey == "" {
		if envKey == "" {
			envKey = "DEEPSEEK_API_KEY"
		}
		apiKey = os.Getenv(envKey)
	}

	return &DeepSeekRaw{
		apiKey:          apiKey,
		baseURL:         "https://api.deepseek.com/beta",
		model:           model,
		thinkingEnabled: enableThinking,

		httpClient:       httpx.NewHTTPClient(120 * time.Second),
		streamHTTPClient: httpx.NewStreamHTTPClient(15 * time.Minute),
	}
}

// ============================================================
// deepseekRawClientConfig — DeepSeek-specific internal config
//
// This private struct extends RawClientConfig with DeepSeek-specific
// fields such as Thinking mode. It is used internally by
// NewDeepSeekRawFromConfig to create a DeepSeekRaw client.
// ============================================================

// DeepseekRawClientConfig extends RawClientConfig with DeepSeek-specific fields.
type DeepseekRawClientConfig struct {
	RawClientConfig

	ThinkingEnabled bool // Thinking mode: true = enabled, false = disabled (default false)
}

// NewDeepSeekRawFromConfig creates a DeepSeekRaw client from a generic RawClientConfig.
func NewDeepSeekRawFromConfig(cfg DeepseekRawClientConfig) *DeepSeekRaw {
	if cfg.APIKey == "" {
		envKey := cfg.EnvKey
		if envKey == "" {
			envKey = "DEEPSEEK_API_KEY"
		}
		cfg.APIKey = os.Getenv(envKey)
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.deepseek.com/beta"
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httpx.NewHTTPClient(120 * time.Second)
	}

	return &DeepSeekRaw{
		apiKey:                cfg.APIKey,
		baseURL:               cfg.BaseURL,
		model:                 cfg.Model,
		thinkingEnabled:       cfg.ThinkingEnabled,
		httpClient:            httpClient,
		streamHTTPClient:      httpx.NewStreamHTTPClient(15 * time.Minute),
		maxToolCallIterations: cfg.MaxToolCallIterations,
	}
}

// Model returns the current model name.
func (c *DeepSeekRaw) Model() string {
	return c.model
}

// GetMaxToolCallIterations returns the maximum number of tool call iterations.
func (c *DeepSeekRaw) GetMaxToolCallIterations() int {
	if c.maxToolCallIterations <= 0 {
		return 5 // default
	}
	return c.maxToolCallIterations
}

// GetUsageInfo returns the token usage information from the most recent API call.
// Returns nil if no call has been made yet.
func (c *DeepSeekRaw) GetUsageInfo() *Usage {
	return c.lastUsage
}

// SetUsageInfo sets the token usage information from an API call.
// This is used by streaming callers to store usage data from the final chunk.
func (c *DeepSeekRaw) SetUsageInfo(usage Usage) {
	c.lastUsage = &usage
}

// storeUsage saves token usage from the most recent API call.
func (c *DeepSeekRaw) storeUsage(usage Usage) {
	c.lastUsage = &usage
}

// ============================================================
// Chat — chat completion (non-streaming)
// ============================================================

// Chat sends a chat message and gets a reply (non-streaming).
// Uses the client's default model.
func (c *DeepSeekRaw) Chat(ctx context.Context, messages []Message) (*ChatCompletionResponse, error) {
	return c.ChatWithOptions(ctx, ChatCompletionRequest{
		Model:    c.model,
		Messages: messages,
	})
}

// ChatWithOptions sends a chat request with custom parameters (non-streaming).
func (c *DeepSeekRaw) ChatWithOptions(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("API client not initialized (API key may be missing)")
	}

	// If model not specified, use client default model
	if req.Model == "" {
		req.Model = c.model
	}
	// Ensure Stream is false for non-streaming
	req.Stream = false

	// Inject thinking mode from client config.
	// Since the DeepSeek API defaults thinking to enabled, we explicitly
	// set "disabled" when thinking is off to ensure it's truly disabled.
	if c.thinkingEnabled {
		req.Thinking = &ThinkingConfig{Type: "enabled"}
	} else {
		req.Thinking = &ThinkingConfig{Type: "disabled"}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request. %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request. %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed. %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response. %w", err)
	}

	// Store usage info
	if chatResp.Usage != nil && (chatResp.Usage.TotalTokens > 0 || chatResp.Usage.PromptTokens > 0 || chatResp.Usage.CompletionTokens > 0) {
		c.storeUsage(*chatResp.Usage)
	}

	return &chatResp, nil
}

// ============================================================
// ChatStream — streaming chat completion
// ============================================================

// ChatStream sends a chat request and returns a stream for reading chunks.
// Uses the client's default model.
func (c *DeepSeekRaw) ChatStream(ctx context.Context, messages []Message) *ChatCompletionChunkDecoder {
	return c.ChatStreamWithOptions(ctx, ChatCompletionRequest{
		Model:    c.model,
		Messages: messages,
	})
}

// ChatStreamWithOptions sends a streaming chat request with custom parameters.
// It uses a dedicated HTTP client with a long timeout to prevent connection drops
// during long pauses between chunks.
func (c *DeepSeekRaw) ChatStreamWithOptions(ctx context.Context, req ChatCompletionRequest) *ChatCompletionChunkDecoder {
	if c.apiKey == "" {
		return newChatCompletionChunkDecoderError(fmt.Errorf("API client not initialized (API key may be missing)"))
	}

	if req.Model == "" {
		req.Model = c.model
	}

	// Enable streaming
	req.Stream = true

	// Inject thinking mode from client config.
	// Since the DeepSeek API defaults thinking to enabled, we explicitly
	// set "disabled" when thinking is off to ensure it's truly disabled.
	if c.thinkingEnabled {
		req.Thinking = &ThinkingConfig{Type: "enabled"}
	} else {
		req.Thinking = &ThinkingConfig{Type: "disabled"}
	}

	// Ensure stream_options with include_usage is set
	if req.StreamOptions == nil {
		req.StreamOptions = &StreamOptions{
			IncludeUsage: true,
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return newChatCompletionChunkDecoderError(fmt.Errorf("failed to marshal request. %w", err))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return newChatCompletionChunkDecoderError(fmt.Errorf("failed to create HTTP request. %w", err))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.streamHTTPClient.Do(httpReq)
	if err != nil {
		return newChatCompletionChunkDecoderError(fmt.Errorf("API request failed. %w", err))
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return newChatCompletionChunkDecoderError(fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody)))
	}

	return NewChatCompletionChunkDecoder(resp.Body)
}

// ============================================================
// ReasoningContent helpers — extract reasoning_content from streaming chunks
// ============================================================

// GetReasoningContent extracts the "reasoning_content" field from a ChatCompletionChunk.
// This is a DeepSeek-specific extension to the standard OpenAI streaming format.
func GetReasoningContent(chunk ChatCompletionChunk) string {
	if len(chunk.Choices) == 0 {
		return ""
	}
	return chunk.Choices[0].Delta.ReasoningContent
}

// ============================================================
// PerformLLMStreamingCall — high-level streaming with tool support
// ============================================================

// PerformLLMStreamingCall performs a streaming LLM call with tool support.
// If the LLM calls a tool (e.g. web_search), it executes the tool via the
// ToolExecutor, appends the tool result, and re-streams with the updated messages.
//
// When the tool call iteration limit is reached, DisableToolChoice() is called
// on the request to prevent the LLM from calling more tools, forcing it to
// answer directly.
//
// Parameters:
//   - ctx: context for cancellation
//   - callback: StreamCallback for receiving streaming events (text, reasoning, etc.)
//   - messages: the conversation messages (will be modified in-place during tool call loops)
//   - tools: tool definitions to pass to the LLM
//   - executor: ToolExecutor that executes tool calls (e.g., web_search)
//
// Returns the final assistant reply content.
func (c *DeepSeekRaw) PerformLLMStreamingCall(
	ctx context.Context,
	callback ToolCallsEvent,
	messages []Message,
	tools []ToolDefinition,
	executor ToolExecutor,
) (fullReply string, err error) {

	maxToolCallIterations := c.GetMaxToolCallIterations()
	toolCallIterations := 0

	for {
		toolCallIterations++

		// Build the streaming request with tools
		req := ChatCompletionRequest{
			Model:    c.model,
			Messages: messages,
		}
		if len(tools) > 0 {
			req.Tools = tools
		}

		// Safety check: prevent infinite tool call loops.
		// When the limit is reached, disable tool choice so the LLM must
		// answer directly, rather than appending a prompt message.
		if toolCallIterations > maxToolCallIterations {
			req.DisableToolChoice()
		}

		stream := c.ChatStreamWithOptions(ctx, req)
		if stream.Err() != nil {
			return "", fmt.Errorf("failed to call LLM API: client not initialized")
		}

		// Collect the full assistant response (text + reasoning + tool calls)
		var replyBuilder strings.Builder
		var reasoningBuilder strings.Builder
		var collectedToolCalls []StreamingToolCall
		finishReason := ""

		for stream.Next() {
			chunk := stream.CurrentChatCompletionChunk()

			// Extract token usage from the final chunk
			if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
				c.SetUsageInfo(*chunk.Usage)
			}

			for _, choice := range chunk.Choices {
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}

				// Collect tool call deltas (streaming tool calls come in chunks)
				for _, tc := range choice.Delta.ToolCalls {
					found := false
					for i := range collectedToolCalls {
						if collectedToolCalls[i].Index == tc.Index {
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
						collectedToolCalls = append(collectedToolCalls, StreamingToolCall{
							Index:     tc.Index,
							ID:        tc.ID,
							Type:      tc.Type,
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						})
					}
				}

				// Extract reasoning_content
				reasoningContent := GetReasoningContent(chunk)
				if reasoningContent != "" {
					if err := callback.OnReasoning(ctx, reasoningContent); err != nil {
						return "", fmt.Errorf("failed to write reasoning event. %w", err)
					}
					reasoningBuilder.WriteString(reasoningContent)
				}

				// Forward text content
				if choice.Delta.Content != "" {
					if err := callback.OnText(ctx, choice.Delta.Content); err != nil {
						return "", fmt.Errorf("failed to write text event. %w", err)
					}
					replyBuilder.WriteString(choice.Delta.Content)
				}
			}
		}

		if err := stream.Err(); err != nil {
			return "", fmt.Errorf("stream error. %w", err)
		}

		// Check if the LLM decided to call a tool
		if finishReason == "tool_calls" && len(collectedToolCalls) > 0 {
			// Build the assistant message with tool calls (for history)
			assistantMsg := Message{
				Role: "assistant",
			}
			if replyBuilder.Len() > 0 {
				assistantMsg.Content = replyBuilder.String()
			}
			if reasoningBuilder.Len() > 0 {
				assistantMsg.ReasoningContent = reasoningBuilder.String()
			}
			for _, tc := range collectedToolCalls {
				assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: ToolCallFunction{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}

			// Append the assistant message first
			messages = append(messages, assistantMsg)

			// Execute each tool call via the ToolExecutor
			for _, tc := range collectedToolCalls {
				// Notify callback about tool call start
				if err := callback.OnToolCallStart(ctx, tc.Name, tc.Arguments); err != nil {
					// Log but continue — non-fatal
				}

				// Execute the tool via the executor
				resultContent, execErr := executor.ExecuteTool(ctx, tc.Name, tc.ID, tc.Arguments)
				if execErr != nil {
					resultContent = i18n.T("tool_execution_failed", map[string]interface{}{
						"Error": execErr,
					})
				}

				// Notify callback about tool call result
				if err := callback.OnToolCallResult(ctx, tc.Name, tc.ID, resultContent); err != nil {
					// Log but continue — non-fatal
				}

				// Append the tool result message
				messages = append(messages, Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    resultContent,
				})
			}

			// Close current stream before looping
			stream.Close()

			// Continue the loop to re-stream with the tool result
			continue
		}

		// Normal completion (stop, length, etc.) — return the reply
		return replyBuilder.String(), nil
	}
}
