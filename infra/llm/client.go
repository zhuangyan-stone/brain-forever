package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"BrainForever/infra/httpx/sse"
)

// ============================================================
// LLMClient — unified interface for LLM API clients
//
// LLMClient abstracts the common operations of an LLM API client,
// including chat completion (non-streaming), streaming chat completion,
// and high-level streaming with tool call support.
//
// Implementations:
//   - DeepSeekRaw (deepseek_raw.go) — uses raw http.Client
//
// Usage:
//
//	var client llm.LLMClient
//	client = llm.NewDeepSeekRaw("", "DEEPSEEK_API_KEY", "deepseek-chat")
//	resp, err := client.Chat(ctx, []llm.Message{...})
//	stream := client.ChatStream(ctx, []llm.Message{...})
// ============================================================

// LLMClient defines the interface for LLM API clients.
type LLMClient interface {
	// Model returns the current model name.
	Model() string

	// GetUsageInfo returns the token usage information from the most recent API call.
	// Returns nil if no call has been made yet.
	GetUsageInfo() *Usage

	// SetUsageInfo sets the token usage information from an API call.
	// This is used by streaming callers to store usage data from the final chunk.
	SetUsageInfo(usage Usage)

	// Chat sends a chat message and gets a reply (non-streaming).
	// Uses the client's default model.
	Chat(ctx context.Context, messages []Message) (*ChatCompletionResponse, error)

	// ChatWithOptions sends a chat request with custom parameters (non-streaming).
	ChatWithOptions(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)

	// ChatStream sends a chat request and returns a stream for reading chunks.
	// Uses the client's default model.
	ChatStream(ctx context.Context, messages []Message) *ChatCompletionChunkDecoder

	// ChatStreamWithOptions sends a streaming chat request with custom parameters.
	ChatStreamWithOptions(ctx context.Context, req ChatCompletionRequest) *ChatCompletionChunkDecoder

	// GetMaxToolCallIterations returns the maximum number of tool call iterations
	// allowed in the streaming loop before forcing a direct answer.
	GetMaxToolCallIterations() int

	// ChatWithPipeline performs a streaming LLM call with tool support.
	// If the LLM calls a tool (e.g. web_search), it executes the tool via the
	// ToolExecutor, appends the tool result, and re-streams with the updated messages.
	//
	// Parameters:
	//   - ctx: context for cancellation
	//   - callback: StreamCallback for receiving streaming events (text, reasoning, etc.)
	//   - messages: the conversation messages (will be modified in-place during tool call loops)
	//   - pipeline: 连接 agent 的前端 (sse client)和后端 (llm-api) 的数据，包括工具调用
	//   - withDeepThink: enable deep thinking/reasoning mode for this request
	//
	// Returns the final assistant reply content and reasoning content.
	ChatWithPipeline(
		ctx context.Context,
		messages []Message,
		pipeline Pipeline,
		withDeepThink bool) (reply string, reasoning string, err error)
}

// ============================================================
// ClientConfig — generic config for creating an LLM client instance
//
// This struct is used by factory functions (e.g. NewDeepSeekRawFromConfig)
// to create a concrete LLM client. DeepSeek-specific fields (e.g. Thinking)
// are handled internally by the implementation.
// ============================================================

// ClientConfig contains common configuration for creating an LLM client.
type ClientConfig struct {
	APIKey                string       // API key, if empty reads from EnvKey env var
	BaseURL               string       // API base URL (e.g., "https://api.deepseek.com")
	Model                 string       // Model name (e.g., "deepseek-chat")
	EnvKey                string       // Environment variable name to read API key from
	HTTPClient            *http.Client // Optional custom HTTP client; nil uses default timeout
	MaxToolCallIterations int          // Max tool call iterations in streaming loop; 0 means default (5)
}

// ============================================================
// Message types — mirror the OpenAI chat completion request/response schema
// ============================================================

// Message represents a single message in the chat completion request.
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"` // DeepSeek-specific
	ToolCallID       string     `json:"tool_call_id,omitempty"`      // For tool result messages
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`        // For assistant messages
}

// ToolCall represents a function tool call from the assistant (non-streaming response).
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// DeltaToolCall represents a tool call delta in a streaming chunk.
// It includes an Index field to correlate partial chunks for the same tool call.
type DeltaToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function,omitempty"`
}

// ToolCallFunction represents the function details in a tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// StreamingToolCall stores tool call data collected from streaming deltas.
// This is a generic structure used across LLM implementations (OpenAI-compatible APIs)
// to accumulate partial tool call fields from streaming chunks into complete tool calls.
type StreamingToolCall struct {
	Index     int
	ID        string
	Type      string
	Name      string
	Arguments string
}

// ChatCompletionRequest is the request body for the chat completion API.
type ChatCompletionRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Stream      bool             `json:"stream,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`

	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`

	// Thinking enables the model's thinking/reasoning mode (DeepSeek-specific).
	// e.g. {"type": "enabled"}
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// StreamOptions controls whether token usage is included in the final streaming chunk.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

func (req *ChatCompletionRequest) DisableToolChoice() {
	// Set ToolChoice to "none" — prevents the LLM from calling any tool.
	req.ToolChoice = json.RawMessage(`"none"`)
}

func (req *ChatCompletionRequest) EnableToolChoice() {
	// Set ToolChoice to "auto" — lets the LLM decide whether to call a tool.
	req.ToolChoice = json.RawMessage(`"auto"`)
}

func (req *ChatCompletionRequest) RequiredToolChoice() {
	// Set ToolChoice to "required" — forces the LLM to call a tool (intelligently)
	// rather than producing a text reply.
	req.ToolChoice = json.RawMessage(`"required"`)
}

func (req *ChatCompletionRequest) ForceToolChoice(functionName string) {
	// Set ToolChoice to a specific function object:
	// {"type": "function", "function": {"name": functionName }}
	req.ToolChoice = json.RawMessage(`{"type":"function","function":{"name":"` + functionName + `"}}`)
}

// ThinkingConfig configures the model's thinking/reasoning mode (DeepSeek-specific).
type ThinkingConfig struct {
	Type string `json:"type"` // "enabled" to enable, "disabled" to disable
}

// StreamOptions configures streaming behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ChatCompletionResponse is the response body from the chat completion API (non-streaming).
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice represents a single choice in the chat completion response.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ============================================================
// Streaming chunk types
// ============================================================

// ChatCompletionChunk represents a single chunk in a streaming response.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ChunkChoice represents a single choice in a streaming chunk.
type ChunkChoice struct {
	Index int `json:"index"`
	Delta struct {
		Role             string          `json:"role,omitempty"`
		Content          string          `json:"content,omitempty"`
		ReasoningContent string          `json:"reasoning_content,omitempty"` // DeepSeek-specific
		ToolCalls        []DeltaToolCall `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

// ============================================================
// ChatCompletionChunkDecoder — typed SSE decoder for LLM streaming chunks
//
// ChatCompletionChunkDecoder embeds sse.SSEReader and provides
// typed access to ChatCompletionChunk values. It overrides Next()
// to handle [DONE] termination signals and parse each SSE data line
// as a JSON ChatCompletionChunk.
// ============================================================

// ChatCompletionChunkDecoder reads SSE streaming chunks and decodes them
// into typed ChatCompletionChunk values.
type ChatCompletionChunkDecoder struct {
	sse.Reader
	currentChunk ChatCompletionChunk
}

// NewChatCompletionChunkDecoder creates a ChatCompletionChunkDecoder
// from an SSE response body.
func NewChatCompletionChunkDecoder(body io.ReadCloser) *ChatCompletionChunkDecoder {
	return &ChatCompletionChunkDecoder{
		Reader: *sse.NewSSEReader(body),
	}
}

// newChatCompletionChunkDecoderError creates a ChatCompletionChunkDecoder
// in an error/done state. Used internally when an API request fails
// before streaming begins.
func newChatCompletionChunkDecoderError(err error) *ChatCompletionChunkDecoder {
	d := &ChatCompletionChunkDecoder{}
	d.SetErr(err)
	return d
}

// Next advances the stream to the next chunk.
// Returns false when the stream is exhausted or an error occurs.
// After Next returns false, call Err() to check for errors.
func (d *ChatCompletionChunkDecoder) Next() bool {
	// Use the embedded SSEReader's Decode to get the raw SSE data line
	data, ok := d.Decode()
	if !ok {
		return false
	}

	// Check for the stream termination signal
	if data == "[DONE]" {
		return false
	}

	// Parse the chunk
	var chunk ChatCompletionChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		d.SetErr(fmt.Errorf("failed to parse streaming chunk. %w", err))
		return false
	}

	d.currentChunk = chunk
	return true
}

// CurrentChatCompletionChunk returns the most recently read chunk.
func (d *ChatCompletionChunkDecoder) CurrentChatCompletionChunk() ChatCompletionChunk {
	return d.currentChunk
}

// ============================================================
// ToolDefinition — defines a tool that the LLM can call
// ============================================================

// ToolDefinition defines a function tool that the LLM can call.
// This mirrors the OpenAI function calling schema.
type ToolDefinition struct {
	Type     string          `json:"type"`
	Function ToolFunctionDef `json:"function"`
}

// ToolFunctionDef defines the function schema for a tool.
type ToolFunctionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
	Strict      *bool       `json:"strict,omitempty"`
}
