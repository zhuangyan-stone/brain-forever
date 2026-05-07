package llm_raw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"BrainOnline/infra/httpx"
)

// ============================================================
// DeepSeekRaw — DeepSeek API client using raw http.Client
//
// This implementation uses net/http directly (no openai-go SDK dependency)
// to provide an alternative way of calling the DeepSeek API.
//
// It also provides high-level streaming methods (PerformLLMStreamingCall,
// PerformLLMStreamingThinkingCall) that handle tool call loops internally,
// delegating actual tool execution to a ToolExecutor interface.
//
// Usage:
//
//	client := NewDeepSeekRaw("", "DEEPSEEK_API_KEY", "deepseek-chat")
//	resp, err := client.Chat(ctx, []Message{
//	    {Role: "user", Content: "Hello"},
//	})
//
//	// Streaming:
//	stream := client.ChatStream(ctx, []Message{
//	    {Role: "user", Content: "Hello"},
//	})
//	for stream.Next() {
//	    chunk := stream.Current()
//	    // process chunk
//	}
//	if stream.Err() != nil { ... }
//
//	// High-level streaming with tool support:
//	result := client.PerformLLMStreamingCall(ctx, callback, messages, tools, executor)
// ============================================================

// ============================================================
// StreamingToolCall — internal type for collecting tool call deltas
// ============================================================

// streamingToolCall stores tool call data collected from streaming deltas.
type streamingToolCall struct {
	Index     int
	ID        string
	Type      string
	Name      string
	Arguments string
}

// ============================================================
// DeepSeekRaw client
// ============================================================

// DeepSeekRaw is a DeepSeek API client that uses raw http.Client.
type DeepSeekRaw struct {
	apiKey           string
	baseURL          string
	model            string
	httpClient       *http.Client
	streamHTTPClient *http.Client
	lastUsage        *Usage // token usage from the most recent API call
}

// NewDeepSeekRaw creates a new DeepSeekRaw client.
//
// apiKey: DeepSeek API Key, if empty reads from the env variable specified by envKey
// envKey: environment variable name, defaults to "DEEPSEEK_API_KEY"
// model:  model name (e.g. "deepseek-v4-flash", "deepseek-chat", "deepseek-reasoner")
func NewDeepSeekRaw(apiKey, envKey, model string) *DeepSeekRaw {
	if apiKey == "" {
		if envKey == "" {
			envKey = "DEEPSEEK_API_KEY"
		}
		apiKey = os.Getenv(envKey)
	}

	return &DeepSeekRaw{
		apiKey:           apiKey,
		baseURL:          "https://api.deepseek.com/beta",
		model:            model,
		httpClient:       httpx.NewHTTPClient(120 * time.Second),
		streamHTTPClient: httpx.NewStreamHTTPClient(15 * time.Minute),
	}
}

// NewDeepSeekRawFromConfig creates a DeepSeekRaw client from a generic config.
type RawClientConfig struct {
	APIKey     string       // API key, if empty reads from EnvKey env var
	BaseURL    string       // API base URL (e.g., "https://api.deepseek.com")
	Model      string       // Model name (e.g., "deepseek-chat")
	EnvKey     string       // Environment variable name to read API key from
	HTTPClient *http.Client // Optional custom HTTP client; nil uses default timeout
}

// NewDeepSeekRawFromConfig creates a DeepSeekRaw client from a RawClientConfig.
func NewDeepSeekRawFromConfig(cfg RawClientConfig) *DeepSeekRaw {
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
		apiKey:           cfg.APIKey,
		baseURL:          cfg.BaseURL,
		model:            cfg.Model,
		httpClient:       httpClient,
		streamHTTPClient: httpx.NewStreamHTTPClient(15 * time.Minute),
	}
}

// Model returns the current model name.
func (c *DeepSeekRaw) Model() string {
	return c.model
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
func (c *DeepSeekRaw) ChatStream(ctx context.Context, messages []Message) *StreamReader {
	return c.ChatStreamWithOptions(ctx, ChatCompletionRequest{
		Model:    c.model,
		Messages: messages,
	})
}

// ChatStreamWithOptions sends a streaming chat request with custom parameters.
// It uses a dedicated HTTP client with a long timeout to prevent connection drops
// during long pauses between chunks.
func (c *DeepSeekRaw) ChatStreamWithOptions(ctx context.Context, req ChatCompletionRequest) *StreamReader {
	if c.apiKey == "" {
		return &StreamReader{err: fmt.Errorf("API client not initialized (API key may be missing)")}
	}

	if req.Model == "" {
		req.Model = c.model
	}

	// Enable streaming
	req.Stream = true

	// Ensure stream_options with include_usage is set
	if req.StreamOptions == nil {
		req.StreamOptions = &StreamOptions{
			IncludeUsage: true,
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return &StreamReader{err: fmt.Errorf("failed to marshal request. %w", err)}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return &StreamReader{err: fmt.Errorf("failed to create HTTP request. %w", err)}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.streamHTTPClient.Do(httpReq)
	if err != nil {
		return &StreamReader{err: fmt.Errorf("API request failed. %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return &StreamReader{err: fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))}
	}

	return newStreamReader(resp.Body)
}

// ============================================================
// StreamReader — reads streaming SSE chunks from the API
// ============================================================

// StreamReader reads streaming chat completion chunks from the API.
// It parses SSE (Server-Sent Events) format and yields ChatCompletionChunk values.
type StreamReader struct {
	scanner *bufio.Scanner
	body    io.Closer
	current ChatCompletionChunk
	err     error
	done    bool
}

// newStreamReader creates a StreamReader from an SSE response body.
func newStreamReader(body io.ReadCloser) *StreamReader {
	return &StreamReader{
		scanner: bufio.NewScanner(body),
		body:    body,
	}
}

// Next advances the stream to the next chunk.
// Returns false when the stream is exhausted or an error occurs.
// After Next returns false, call Err() to check if there was an error.
func (sr *StreamReader) Next() bool {
	if sr.done {
		return false
	}

	for sr.scanner.Scan() {
		line := sr.scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// Skip non-data lines (e.g., "event: ...")
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := line[6:] // Strip "data: " prefix

		// Check for the stream termination signal
		if data == "[DONE]" {
			sr.done = true
			return false
		}

		// Parse the chunk
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			sr.err = fmt.Errorf("failed to parse streaming chunk. %w", err)
			sr.done = true
			return false
		}

		sr.current = chunk
		return true
	}

	// Check for scanner error
	if err := sr.scanner.Err(); err != nil {
		sr.err = fmt.Errorf("stream scanner error. %w", err)
	}
	sr.done = true
	return false
}

// Current returns the most recently read chunk.
func (sr *StreamReader) Current() ChatCompletionChunk {
	return sr.current
}

// Err returns any error encountered during streaming.
func (sr *StreamReader) Err() error {
	return sr.err
}

// Close closes the underlying response body.
func (sr *StreamReader) Close() error {
	if sr.body != nil {
		return sr.body.Close()
	}
	return nil
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
// maxToolCallIterations — safety limit to prevent infinite tool call loops
// ============================================================

const maxToolCallIterations = 5

// ============================================================
// PerformLLMStreamingCall — high-level streaming with tool support
// ============================================================

// PerformLLMStreamingCall performs a streaming LLM call with tool support.
// If the LLM calls a tool (e.g. web_search), it executes the tool via the
// ToolExecutor, appends the tool result, and re-streams with the updated messages.
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
	callback StreamCallback,
	messages []Message,
	tools []ToolDefinition,
	executor ToolExecutor,
) (fullReply string, err error) {

	toolCallIterations := 0

	for {
		// Safety check: prevent infinite tool call loops
		toolCallIterations++
		if toolCallIterations > maxToolCallIterations {
			return "", fmt.Errorf("tool call iteration limit (%d) exceeded — possible infinite loop", maxToolCallIterations)
		}

		// Build the streaming request with tools
		req := ChatCompletionRequest{
			Model:    c.model,
			Messages: messages,
		}
		if len(tools) > 0 {
			req.Tools = tools
		}

		stream := c.ChatStreamWithOptions(ctx, req)
		if stream.err != nil {
			return "", fmt.Errorf("failed to call LLM API: client not initialized")
		}

		// Collect the full assistant response (text + reasoning + tool calls)
		var replyBuilder strings.Builder
		var reasoningBuilder strings.Builder
		var collectedToolCalls []streamingToolCall
		finishReason := ""

		for stream.Next() {
			chunk := stream.Current()

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
						collectedToolCalls = append(collectedToolCalls, streamingToolCall{
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
					resultContent = fmt.Sprintf("Tool execution failed: %v", execErr)
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

// ============================================================
// PerformLLMStreamingThinkingCall — deep-thinking mode with tool support
// ============================================================

// PerformLLMStreamingThinkingCall performs a streaming LLM call in deep-thinking
// mode. It streams reasoning_content to the callback as "reasoning" events,
// and supports multiple tool calls across multiple sub-turns.
//
// The function follows the DeepSeek thinking mode protocol:
//   - reasoning_content and content can appear in the same or different chunks
//   - assistant messages always include reasoning_content (API ignores it when
//     there were no tool calls in that turn)
//   - tool results trigger re-entry into the thinking loop
//
// Parameters:
//   - ctx: context for cancellation
//   - callback: StreamCallback for receiving streaming events
//   - messages: the conversation messages (will be modified in-place during tool call loops)
//   - tools: tool definitions to pass to the LLM
//   - executor: ToolExecutor that executes tool calls
//
// Returns the final assistant reply content.
func (c *DeepSeekRaw) PerformLLMStreamingThinkingCall(
	ctx context.Context,
	callback StreamCallback,
	messages []Message,
	tools []ToolDefinition,
	executor ToolExecutor,
) (fullReply string, err error) {

	toolCallIterations := 0

	for {
		// Safety check: prevent infinite tool call loops
		toolCallIterations++
		if toolCallIterations > maxToolCallIterations {
			return "", fmt.Errorf("tool call iteration limit (%d) exceeded in thinking mode — possible infinite loop", maxToolCallIterations)
		}

		// Build the streaming request with thinking mode enabled
		req := ChatCompletionRequest{
			Model:    c.model,
			Messages: messages,
		}
		if len(tools) > 0 {
			req.Tools = tools
		}

		// For thinking mode, we need to include the "thinking" field in the request body.
		// We use a custom JSON marshal to inject it since ChatCompletionRequest doesn't
		// have a Thinking field by default.
		stream := c.chatStreamWithThinking(ctx, req)
		if stream.err != nil {
			return "", fmt.Errorf("failed to call LLM API (thinking mode): client not initialized")
		}

		// Collect the full assistant response (reasoning + content + tool calls)
		var reasoningBuilder strings.Builder
		var replyBuilder strings.Builder
		var collectedToolCalls []streamingToolCall
		finishReason := ""

		for stream.Next() {
			chunk := stream.Current()

			// Extract token usage from the final chunk
			if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
				c.SetUsageInfo(*chunk.Usage)
			}

			for _, choice := range chunk.Choices {
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}

				// Collect tool call deltas
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
						collectedToolCalls = append(collectedToolCalls, streamingToolCall{
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
			return "", fmt.Errorf("stream error (thinking mode). %w", err)
		}

		// Check if the LLM decided to call a tool
		if finishReason == "tool_calls" && len(collectedToolCalls) > 0 {
			// Build the assistant message with tool calls
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
					resultContent = fmt.Sprintf("Tool execution failed: %v", execErr)
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

// ============================================================
// Internal helpers
// ============================================================

// chatStreamWithThinking sends a streaming request with the "thinking" field
// enabled for DeepSeek thinking mode. It uses a custom JSON marshal to inject
// the thinking field since ChatCompletionRequest doesn't have it by default.
func (c *DeepSeekRaw) chatStreamWithThinking(ctx context.Context, req ChatCompletionRequest) *StreamReader {
	if c.apiKey == "" {
		return &StreamReader{err: fmt.Errorf("API client not initialized (API key may be missing)")}
	}

	if req.Model == "" {
		req.Model = c.model
	}

	req.Stream = true
	if req.StreamOptions == nil {
		req.StreamOptions = &StreamOptions{
			IncludeUsage: true,
		}
	}

	// Build the base request body
	baseBody, err := json.Marshal(req)
	if err != nil {
		return &StreamReader{err: fmt.Errorf("failed to marshal request. %w", err)}
	}

	// Inject the "thinking" field into the JSON body
	var bodyMap map[string]any
	if err := json.Unmarshal(baseBody, &bodyMap); err != nil {
		return &StreamReader{err: fmt.Errorf("failed to unmarshal request body. %w", err)}
	}
	bodyMap["thinking"] = map[string]any{"type": "enabled"}

	finalBody, err := json.Marshal(bodyMap)
	if err != nil {
		return &StreamReader{err: fmt.Errorf("failed to marshal final request body. %w", err)}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(finalBody))
	if err != nil {
		return &StreamReader{err: fmt.Errorf("failed to create HTTP request. %w", err)}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.streamHTTPClient.Do(httpReq)
	if err != nil {
		return &StreamReader{err: fmt.Errorf("API request failed. %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return &StreamReader{err: fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))}
	}

	return newStreamReader(resp.Body)
}
