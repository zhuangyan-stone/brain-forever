package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"BrainOnline/infra/httpx"
	"BrainOnline/infra/i18n"
)

// ============================================================
// DeepSeek — DeepSeek API client using raw http.Client
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

// DeepSeekClient is a DeepSeek API client that uses raw http.Client.
type DeepSeekClient struct {
	apiKey                string
	baseURL               string
	model                 string
	httpClient            *http.Client
	streamHTTPClient      *http.Client
	lastUsage             *Usage // token usage from the most recent API call
	maxToolCallIterations int    // max tool call iterations; 0 means default (5)
}

// NewDeepSeekClient creates a new DeepSeekRaw client.
//
// apiKey: DeepSeek API Key, if empty reads from the env variable specified by envKey
// envKey: environment variable name, defaults to "DEEPSEEK_API_KEY"
// model:  model name (e.g. "deepseek-v4-flash", "deepseek-chat", "deepseek-reasoner")
func NewDeepSeekClient(apiKey, envKey, model string) *DeepSeekClient {
	if apiKey == "" {
		if envKey == "" {
			envKey = "DEEPSEEK_API_KEY"
		}
		apiKey = os.Getenv(envKey)
	}

	return &DeepSeekClient{
		apiKey:  apiKey,
		baseURL: "https://api.deepseek.com/beta",
		model:   model,

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

// DeepseekClientConfig extends RawClientConfig with DeepSeek-specific fields.
type DeepseekClientConfig struct {
	RawClientConfig
}

// NewDeepSeekClientFromConfig creates a DeepSeekRaw client from a generic RawClientConfig.
func NewDeepSeekClientFromConfig(cfg DeepseekClientConfig) *DeepSeekClient {
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

	return &DeepSeekClient{
		apiKey:                cfg.APIKey,
		baseURL:               cfg.BaseURL,
		model:                 cfg.Model,
		httpClient:            httpClient,
		streamHTTPClient:      httpx.NewStreamHTTPClient(15 * time.Minute),
		maxToolCallIterations: cfg.MaxToolCallIterations,
	}
}

// Model returns the current model name.
func (c *DeepSeekClient) Model() string {
	return c.model
}

// GetMaxToolCallIterations returns the maximum number of tool call iterations.
func (c *DeepSeekClient) GetMaxToolCallIterations() int {
	if c.maxToolCallIterations <= 0 {
		return 5 // default
	}
	return c.maxToolCallIterations
}

// GetUsageInfo returns the token usage information from the most recent API call.
// Returns nil if no call has been made yet.
func (c *DeepSeekClient) GetUsageInfo() *Usage {
	return c.lastUsage
}

// SetUsageInfo sets the token usage information from an API call.
// This is used by streaming callers to store usage data from the final chunk.
func (c *DeepSeekClient) SetUsageInfo(usage Usage) {
	c.lastUsage = &usage
}

// storeUsage saves token usage from the most recent API call.
func (c *DeepSeekClient) storeUsage(usage Usage) {
	c.lastUsage = &usage
}

// ============================================================
// Chat — chat completion (non-streaming)
// ============================================================

// Chat sends a chat message and gets a reply (non-streaming).
// Uses the client's default model.
func (c *DeepSeekClient) Chat(ctx context.Context, messages []Message) (*ChatCompletionResponse, error) {
	return c.ChatWithOptions(ctx, ChatCompletionRequest{
		Model:    c.model,
		Messages: messages,
	})
}

// ChatWithOptions sends a chat request with custom parameters (non-streaming).
func (c *DeepSeekClient) ChatWithOptions(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("API client not initialized (API key may be missing)")
	}

	// If model not specified, use client default model
	if req.Model == "" {
		req.Model = c.model
	}
	// Ensure Stream is false for non-streaming
	req.Stream = false

	// Thinking mode is now controlled per-request by the caller via
	// PerformLLMStreamingCall's deepThink parameter. For the non-streaming
	// Chat/ChatWithOptions path, default to enabled (matching DeepSeek API's default).
	if req.Thinking == nil {
		req.Thinking = &ThinkingConfig{Type: "enabled"}
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
func (c *DeepSeekClient) ChatStream(ctx context.Context, messages []Message) *ChatCompletionChunkDecoder {
	return c.ChatStreamWithOptions(ctx, ChatCompletionRequest{
		Model:    c.model,
		Messages: messages,
	})
}

// ChatStreamWithOptions sends a streaming chat request with custom parameters.
// It uses a dedicated HTTP client with a long timeout to prevent connection drops
// during long pauses between chunks.
func (c *DeepSeekClient) ChatStreamWithOptions(ctx context.Context, req ChatCompletionRequest) *ChatCompletionChunkDecoder {
	if c.apiKey == "" {
		return newChatCompletionChunkDecoderError(fmt.Errorf("API client not initialized (API key may be missing)"))
	}

	if req.Model == "" {
		req.Model = c.model
	}

	// Enable streaming
	req.Stream = true

	// Thinking mode is now set per-request by PerformLLMStreamingCall.
	// If the caller hasn't set it (e.g. direct ChatStreamWithOptions usage),
	// default to enabled (matching DeepSeek API's default).
	if req.Thinking == nil {
		req.Thinking = &ThinkingConfig{Type: "enabled"}
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

// GetReasoningContentFromChoice extracts the "reasoning_content" field from a ChunkChoice.
// This is a DeepSeek-specific extension to the standard OpenAI streaming format.
func GetReasoningContentFromChoice(choice ChunkChoice) string {
	return choice.Delta.ReasoningContent
}

// ============================================================
// ChatWithPipeline — high-level streaming with tool support
// ============================================================

// ChatWithPipeline performs a streaming LLM call with tool support.
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
// Returns the final assistant reply content and reasoning content.
func (c *DeepSeekClient) ChatWithPipeline(
	ctx context.Context,
	messages []Message,
	pipeline Pipeline,
	withDeepThink bool) (reply string, reasoning string, err error) {

	maxToolCallIterations := c.GetMaxToolCallIterations()
	toolCallIterations := 0

	// 取出所有准备给LLM看的函数定义
	toolDefs := pipeline.GetToolDefines()

	for {
		toolCallIterations++

		// Build the streaming request with tools
		req := ChatCompletionRequest{
			Model:    c.model,
			Messages: messages,
		}
		if len(toolDefs) > 0 {
			req.Tools = toolDefs
		}

		// Set thinking mode based on the per-request deepThink flag.
		// ChatStreamWithOptions will respect this if already set.
		req.Thinking = &ThinkingConfig{Type: "enabled"}
		if !withDeepThink {
			req.Thinking.Type = "disabled"
		}

		// Safety check: prevent infinite tool call loops.
		// When the limit is reached, disable tool choice so the LLM must
		// answer directly, rather than appending a prompt message.
		if toolCallIterations > maxToolCallIterations {
			req.DisableToolChoice()
		}

		// 开始连接
		stream := c.ChatStreamWithOptions(ctx, req)
		if stream.Err() != nil {
			return "", "", fmt.Errorf("failed to call LLM API: client not initialized")
		}

		// Collect the full assistant response (text/reply + reasoning + tool calls)
		var replyBuilder strings.Builder
		var reasoningBuilder strings.Builder
		var toolCalls []StreamingToolCall // llm 发起的函数调用信息，可能有多个
		finishReason := ""

		for stream.Next() {
			chunk := stream.CurrentChatCompletionChunk()

			// Extract token usage from the final chunk
			if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
				c.SetUsageInfo(*chunk.Usage)
			}

			if len(chunk.Choices) == 0 {
				err = errors.New("chunk's choices is empty")
				return
			}

			choice := chunk.Choices[0]

			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}

			// Collect tool call deltas (streaming tool calls come in chunks)
			for _, tc := range choice.Delta.ToolCalls {
				toolCalls = mergeToolCall(toolCalls, tc)
			}

			// Extract reasoning_content
			reasoningContent := GetReasoningContentFromChoice(choice)
			if reasoningContent != "" {
				pipeline.OnReasoning(reasoningContent)
				// 收集到 reasoning
				reasoningBuilder.WriteString(reasoningContent)
			}

			// Forward text content
			if choice.Delta.Content != "" {
				pipeline.OnText(choice.Delta.Content)
				// 收集到 reply 中
				replyBuilder.WriteString(choice.Delta.Content)
			}
		}

		if err := stream.Err(); err != nil {
			return "", "", fmt.Errorf("stream error. %w", err)
		}

		// Check if the LLM decided to call a tool
		if finishReason == "tool_calls" && len(toolCalls) > 0 {
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

			// 往助手消息中，添加对工具的调用
			for _, tc := range toolCalls {
				assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: ToolCallFunction{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}

			// Append the assistant message first (before tool messages)
			messages = append(messages, assistantMsg)

			// Execute each tool call via the ToolCallser
			for _, tc := range toolCalls {
				resultContent := ""

				if pendingErr := pipeline.Pending(tc.ID, tc.Name, tc.Arguments); pendingErr != nil {
					resultContent = i18n.T("set_tool_argument_faild", map[string]interface{}{
						"Error": pendingErr,
					})
				} else {
					// Execute the tool via the caller
					var execErr error
					resultContent, execErr = pipeline.Call(tc.ID, tc.Name)

					if execErr != nil {
						resultContent = i18n.T("tool_execution_failed", map[string]interface{}{
							"Error": execErr,
						})
					}

					// Append the tool result message
					messages = append(messages, Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    resultContent,
					})
				}
			}

			// Close current stream before looping
			stream.Close()

			// Continue the loop to re-stream with the tool result
			continue
		}

		// Normal completion (stop, length, etc.) — return the reply and reasoning
		return replyBuilder.String(), reasoningBuilder.String(), nil
	}
}

// mergeToolCall merges a streaming tool call delta into the accumulated toolCalls slice.
// Streaming tool calls arrive in chunks; chunks with the same Index belong to the same
// logical function call. This function either updates the existing entry (by appending
// arguments and filling in missing fields) or appends a new entry for a first-seen Index.
func mergeToolCall(toolCalls []StreamingToolCall, delta DeltaToolCall) []StreamingToolCall {
	for i := range toolCalls {
		if toolCalls[i].Index == delta.Index {
			if delta.Function.Name != "" {
				toolCalls[i].Name = delta.Function.Name
			}
			if delta.Function.Arguments != "" {
				toolCalls[i].Arguments += delta.Function.Arguments
			}
			if delta.ID != "" {
				toolCalls[i].ID = delta.ID
			}
			if delta.Type != "" {
				toolCalls[i].Type = delta.Type
			}
			return toolCalls
		}
	}
	return append(toolCalls, StreamingToolCall{
		Index:     delta.Index,
		ID:        delta.ID,
		Type:      delta.Type,
		Name:      delta.Function.Name,
		Arguments: delta.Function.Arguments,
	})
}
