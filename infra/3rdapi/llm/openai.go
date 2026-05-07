package llm

// ============================================================
// OpenAI client — thin wrapper around OpenAICompatibleClient
// ============================================================

// NewOpenAIClient creates an LLM client for the official OpenAI API.
//
// apiKey: OpenAI API Key, if empty reads from the env variable specified by envKey
// envKey: environment variable name, defaults to "OPENAI_API_KEY"
// model:  model name (e.g. "gpt-4", "gpt-4o", "gpt-3.5-turbo")
//
// Usage:
//
//	client := NewOpenAIClient("", "OPENAI_API_KEY", "gpt-4")
//	stream := client.ChatStream(ctx, []openai.ChatCompletionMessageParamUnion{
//	    openai.UserMessage("Hello"),
//	})
func NewOpenAIClient(apiKey, envKey, model string) *OpenAICompatibleClient {
	return NewOpenAICompatibleClient(apiKey, envKey, "https://api.openai.com", model)
}

// NewOpenAIClientFromConfig creates an OpenAI client from a generic ClientConfig.
func NewOpenAIClientFromConfig(cfg ClientConfig) *OpenAICompatibleClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}
	return NewOpenAICompatibleClientFromConfig(cfg)
}
