package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"

	"BrainOnline/infra/httpx"
	"BrainOnline/toolset"
)

// ============================================================
// OpenAICompatibleClient — generic client for any OpenAI-compatible LLM API
//
// This implementation uses the official openai-go library (github.com/openai/openai-go)
// as the underlying HTTP client.
//
// Usage:
//
//	client := NewOpenAICompatibleClient("", "OPENAI_API_KEY",
//	    "https://api.openai.com", "gpt-4")
//	stream := client.ChatStream(ctx, openai.ChatCompletionNewParams{
//	    Model: openai.ChatModelGPT4o,
//	    Messages: []openai.ChatCompletionMessageParamUnion{
//	        openai.UserMessage("Hello"),
//	    },
//	})
//	for stream.Next() {
//	    chunk := stream.Current()
//	    // process chunk
//	}
//	if stream.Err() != nil { ... }
// ============================================================

// OpenAICompatibleClient wraps openai-go's Client with convenience features
// such as auto-loading API keys from environment variables and custom HTTP client setup.
type OpenAICompatibleClient struct {
	client     *openai.Client
	streamOpts []option.RequestOption // options for streaming (no timeout)
	model      string
	lastUsage  *openai.CompletionUsage // token usage from the most recent API call
}

// NewOpenAICompatibleClient creates a generic OpenAI-compatible LLM client.
//
// apiKey: API key, if empty reads from the env variable specified by envKey
// envKey: environment variable name (e.g. "OPENAI_API_KEY", "DEEPSEEK_API_KEY")
// baseURL: API base URL (e.g. "https://api.openai.com", "https://api.deepseek.com")
// model: model name (e.g. "gpt-4", "deepseek-chat")
func NewOpenAICompatibleClient(apiKey, envKey, baseURL, model string) *OpenAICompatibleClient {
	if apiKey == "" {
		if envKey == "" {
			envKey = "OPENAI_API_KEY"
		}
		apiKey = os.Getenv(envKey)
	}

	httpClient := httpx.NewHTTPClient(120 * time.Second)
	streamHTTPClient := httpx.NewStreamHTTPClient(15 * time.Minute)

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(httpClient),
	)

	// Streaming options: same API key and base URL, but with a long-timeout HTTP client
	streamOpts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(streamHTTPClient),
	}

	return &OpenAICompatibleClient{
		client:     &client,
		streamOpts: streamOpts,
		model:      model,
	}
}

// NewOpenAICompatibleClientFromConfig creates an OpenAI-compatible client from a generic ClientConfig.
func NewOpenAICompatibleClientFromConfig(cfg ClientConfig) *OpenAICompatibleClient {
	if cfg.APIKey == "" {
		envKey := cfg.EnvKey
		if envKey == "" {
			envKey = "OPENAI_API_KEY"
		}
		cfg.APIKey = os.Getenv(envKey)
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httpx.NewHTTPClient(120 * time.Second)
	}

	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
		option.WithHTTPClient(httpClient),
	)

	streamHTTPClient := httpx.NewStreamHTTPClient(15 * time.Minute)
	streamOpts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
		option.WithHTTPClient(streamHTTPClient),
	}

	return &OpenAICompatibleClient{
		client:     &client,
		streamOpts: streamOpts,
		model:      cfg.Model,
	}
}

// Model returns the current model name
func (c *OpenAICompatibleClient) Model() string {
	return c.model
}

// GetUsageInfo returns the token usage information from the most recent API call.
// Returns nil if no call has been made yet.
func (c *OpenAICompatibleClient) GetUsageInfo() *openai.CompletionUsage {
	return c.lastUsage
}

// SetUsageInfo sets the token usage information from an API call.
// This is used by streaming callers to store usage data from the final chunk.
func (c *OpenAICompatibleClient) SetUsageInfo(usage openai.CompletionUsage) {
	c.lastUsage = &usage
}

// storeUsage saves token usage from the most recent API call.
func (c *OpenAICompatibleClient) storeUsage(usage openai.CompletionUsage) {
	c.lastUsage = &usage
}

// ============================================================
// Chat — chat completion (non-streaming)
// ============================================================

// Chat sends a chat message and gets a reply (non-streaming).
// Uses the client's default model.
func (c *OpenAICompatibleClient) Chat(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion) (*openai.ChatCompletion, error) {
	return c.ChatWithOptions(ctx, openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(c.model),
		Messages: messages,
	})
}

// ChatWithOptions sends a chat request with custom parameters (non-streaming).
func (c *OpenAICompatibleClient) ChatWithOptions(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if c.client == nil {
		return nil, fmt.Errorf("API client not initialized (API key may be missing)")
	}

	// If model not specified, use client default model
	if params.Model == "" {
		params.Model = shared.ChatModel(c.model)
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("API request failed. %w", err)
	}

	// Store usage info
	if resp.Usage.TotalTokens > 0 || resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		c.storeUsage(resp.Usage)
	}

	return resp, nil
}

// ============================================================
// ChatStream — streaming chat completion
// ============================================================

// ChatStream sends a chat request and returns a stream for reading chunks.
// Uses the client's default model.
func (c *OpenAICompatibleClient) ChatStream(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion) *ssestream.Stream[openai.ChatCompletionChunk] {
	return c.ChatStreamWithOptions(ctx, openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(c.model),
		Messages: messages,
	})
}

// ChatStreamWithOptions sends a streaming chat request with custom parameters.
// It uses a dedicated HTTP client with NO Timeout to prevent connection drops
// during long pauses between chunks (e.g., when debugging with a debugger).
//
// The opts parameter allows passing additional request options, such as
// option.WithJSONSet("thinking", map[string]any{"type": "enabled"}) for DeepSeek thinking mode.
func (c *OpenAICompatibleClient) ChatStreamWithOptions(
	ctx context.Context,
	params openai.ChatCompletionNewParams,
	opts ...option.RequestOption,
) *ssestream.Stream[openai.ChatCompletionChunk] {
	if c.client == nil {
		return nil // caller should check for nil
	}

	if params.Model == "" {
		params.Model = shared.ChatModel(c.model)
	}

	// Ensure stream_options with include_usage is set
	if param.IsOmitted(params.StreamOptions) {
		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: param.NewOpt(true),
		}
	}

	// Combine stream-specific options with any extra options
	allOpts := append(c.streamOpts, opts...)

	stream := c.client.Chat.Completions.NewStreaming(ctx, params, allOpts...)
	return stream
}

// ============================================================
// ReasoningContent helpers — extract reasoning_content from streaming chunks
// ============================================================

// GetReasoningContent extracts the "reasoning_content" field from a ChatCompletionChunk
// using RawJSON parsing. This is needed because openai-go's ChatCompletionChunkChoiceDelta
// does not have a built-in ReasoningContent field (it's a DeepSeek-specific extension).
func GetReasoningContent(chunk openai.ChatCompletionChunk) string {
	if len(chunk.Choices) == 0 {
		return ""
	}
	return getReasoningContentFromDelta(chunk.Choices[0].Delta.RawJSON())
}

// getReasoningContentFromDelta parses the raw JSON of a delta to extract reasoning_content.
func getReasoningContentFromDelta(rawJSON string) string {
	if rawJSON == "" {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &data); err != nil {
		return ""
	}
	rc, ok := data["reasoning_content"]
	if !ok {
		return ""
	}
	s, ok := rc.(string)
	if !ok {
		return ""
	}
	return s
}

// ============================================================
// EstimateTokensByRates — client-side token estimation heuristic
// ============================================================

// EstimateTokensByRates estimates the token count of the given text using a heuristic
// for mixed Chinese/English content.
//
// LLM tokenizers (e.g., GPT, Claude, DeepSeek) use subword tokenization:
//   - Chinese characters: each character typically consumes 1.5~2 tokens.
//     We use 1.75 as a middle-ground estimate.
//   - English words: split into subword pieces. A typical English word
//     averages ~1.1 tokens (common words like "the"=1, longer words=2~3).
//   - Digits: consecutive digit groups average ~0.5 tokens per digit
//     (e.g., "12345" → 2-3 tokens).
//   - Whitespace: spaces/tabs/newlines are usually merged into adjacent
//     tokens and don't consume tokens on their own.
//
// The algorithm:
//  1. Count CJK characters (Chinese, Japanese, Korean) → ×1.75
//  2. Count digit groups → ×0.5 per digit
//  3. Split remaining text by whitespace → count English "words" → ×1.1
//  4. Sum and round up.
func (c *OpenAICompatibleClient) EstimateTokensByRates(content string, cjkTokenRate, wordTokenRate, digitTokenRate float64) int {
	var (
		cjkCount   int
		digitCount int
		wordCount  int
	)

	// Process the string rune by rune to classify characters.
	runes := []rune(content)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case toolset.IsCJK(r):
			cjkCount++
			i++
		case r >= '0' && r <= '9':
			// Consume a consecutive digit run
			j := i
			for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
				j++
			}
			digitCount += j - i
			i = j
		case toolset.IsWhitespace(r):
			// Skip whitespace — it doesn't consume tokens on its own
			i++
		default:
			// Everything else is treated as part of a "word" token.
			// Consume until we hit whitespace, CJK, or digit.
			j := i
			for j < len(runes) && !toolset.IsCJK(runes[j]) && !(runes[j] >= '0' && runes[j] <= '9') && !toolset.IsWhitespace(runes[j]) {
				j++
			}
			if j > i {
				wordCount++
			}
			i = j
		}
	}

	total := int(math.Ceil(float64(cjkCount)*cjkTokenRate +
		float64(digitCount)*digitTokenRate +
		float64(wordCount)*wordTokenRate))

	return total
}

func (c *OpenAICompatibleClient) EstimateTokens(content string) int {
	const (
		cjkTokenRate   = 1.75 // tokens per CJK character
		wordTokenRate  = 1.1  // tokens per whitespace-delimited word
		digitTokenRate = 0.5  // tokens per digit character
	)

	return c.EstimateTokensByRates(content, cjkTokenRate, wordTokenRate, digitTokenRate)
}
