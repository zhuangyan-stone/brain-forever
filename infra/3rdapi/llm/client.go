package llm

import (
	"net/http"
)

// ============================================================
// ClientConfig — generic configuration for creating AI clients
// ============================================================

// ClientConfig holds the common configuration for creating an LLM API client.
// Each provider (DeepSeek, OpenAI, etc.) reads relevant fields from this config.
//
// Usage:
//
//	cfg := llm.ClientConfig{
//	    APIKey:  "",
//	    BaseURL: "https://api.deepseek.com",
//	    Model:   "deepseek-chat",
//	    EnvKey:  "DEEPSEEK_API_KEY",
//	}
//	client := llm.NewDeepSeekClientFromConfig(cfg)
type ClientConfig struct {
	APIKey     string       // API key, if empty reads from EnvKey env var
	BaseURL    string       // API base URL (e.g., "https://api.deepseek.com")
	Model      string       // Model name (e.g., "deepseek-chat", "gpt-4")
	EnvKey     string       // Environment variable name to read API key from
	HTTPClient *http.Client // Optional custom HTTP client; nil uses default timeout
}
