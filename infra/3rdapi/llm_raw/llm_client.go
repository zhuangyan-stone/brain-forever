package llm_raw

import (
	"context"
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
//	var client llm_raw.LLMClient
//	client = llm_raw.NewDeepSeekRaw("", "DEEPSEEK_API_KEY", "deepseek-chat")
//	resp, err := client.Chat(ctx, []llm_raw.Message{...})
//	stream := client.ChatStream(ctx, []llm_raw.Message{...})
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
	ChatStream(ctx context.Context, messages []Message) *StreamReader

	// ChatStreamWithOptions sends a streaming chat request with custom parameters.
	ChatStreamWithOptions(ctx context.Context, req ChatCompletionRequest) *StreamReader

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
	PerformLLMStreamingCall(
		ctx context.Context,
		callback StreamCallback,
		messages []Message,
		tools []ToolDefinition,
		executor ToolExecutor,
	) (fullReply string, err error)

	// PerformLLMStreamingThinkingCall performs a streaming LLM call in deep-thinking
	// mode. It streams reasoning_content to the callback as "reasoning" events,
	// and supports multiple tool calls across multiple sub-turns.
	PerformLLMStreamingThinkingCall(
		ctx context.Context,
		callback StreamCallback,
		messages []Message,
		tools []ToolDefinition,
		executor ToolExecutor,
	) (fullReply string, err error)
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

// ChatCompletionRequest is the request body for the chat completion API.
type ChatCompletionRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Stream      bool             `json:"stream,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`

	// StreamOptions controls whether token usage is included in the final streaming chunk.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
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

// ============================================================
// ToolExecutor interface — decouples tool execution from the LLM client
// ============================================================

// ToolExecutor is the interface for executing tool calls made by the LLM.
// The caller (e.g., ChatHandler) implements this interface to provide
// concrete tool implementations (e.g., web_search).
//
// ExecuteTool receives a tool call from the LLM and returns the result
// content that will be sent back to the LLM as a tool result message.
type ToolExecutor interface {
	// ExecuteTool executes a tool call and returns the result content.
	// toolName is the function name (e.g., "web_search").
	// arguments is the JSON string of the tool call arguments.
	// The returned string is the tool result content sent back to the LLM.
	ExecuteTool(ctx context.Context, toolName string, toolCallID string, arguments string) (resultContent string, err error)
}

// ============================================================
// StreamCallback interface — decouples streaming output from the LLM client
// ============================================================

// StreamCallback defines callbacks for streaming events during LLM calls.
// The caller implements this to receive streaming content (e.g., to forward
// to an SSE writer).
type StreamCallback interface {
	// OnText is called when a text content delta is received from the LLM.
	OnText(ctx context.Context, delta string) error

	// OnReasoning is called when a reasoning_content delta is received (DeepSeek-specific).
	OnReasoning(ctx context.Context, delta string) error

	// OnToolCallStart is called when the LLM starts calling a tool.
	// This can be used to notify the frontend (e.g., send a "web_search" SSE event).
	OnToolCallStart(ctx context.Context, toolName string, arguments string) error

	// OnToolCallResult is called after a tool has been executed.
	// This can be used to notify the frontend about tool execution results.
	OnToolCallResult(ctx context.Context, toolName string, toolCallID string, result string) error

	// OnError is called when an error occurs during streaming.
	OnError(ctx context.Context, err error) error
}
