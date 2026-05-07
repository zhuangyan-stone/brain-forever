package llm

// ============================================================
// DeepSeek client — thin wrapper around OpenAICompatibleClient
// ============================================================

// NewDeepSeekClient creates an LLM client for the DeepSeek API.
//
// apiKey: DeepSeek API Key, if empty reads from the env variable specified by envKey
// envKey: environment variable name, defaults to "DEEPSEEK_API_KEY"
// model:  model name (e.g. "deepseek-v4-flash", "deepseek-chat", "deepseek-reasoner")
//
// Usage:
//
//	client := NewDeepSeekClient("", "DEEPSEEK_API_KEY", "deepseek-v4-flash")
//	stream := client.ChatStream(ctx, []openai.ChatCompletionMessageParamUnion{
//	    openai.UserMessage("Hello"),
//	})
func NewDeepSeekClient(apiKey, envKey, model string) *OpenAICompatibleClient {
	return NewOpenAICompatibleClient(apiKey, envKey, "https://api.deepseek.com/beta", model)
}

// NewDeepSeekClientFromConfig creates a DeepSeek client from a generic ClientConfig.
func NewDeepSeekClientFromConfig(cfg ClientConfig) *OpenAICompatibleClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.deepseek.com/beta"
	}
	if cfg.Model == "" {
		cfg.Model = "deepseek-v4-flash"
	}
	return NewOpenAICompatibleClientFromConfig(cfg)
}
