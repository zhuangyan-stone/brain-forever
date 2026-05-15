package config

// ============================================================
// Config — centralized configuration for the BrainForever agent
//
// This struct holds the configuration for the four core objects
// that are initialized in agent/init.go:
//   - Embedder (text embedding)
//   - VectorStore (knowledge base / trait search)
//   - LLMClient (chat completion)
//   - WebSearchClient (online search)
// ============================================================

// Config is the top-level configuration for the agent layer.
type Config struct {
	Logger      LoggerConfig
	Embedder    EmbedderConfig
	VectorStore VectorStoreConfig
	ChatLLM     ChatLLMConfig
	WebSearch   WebSearchConfig
}

// LoggerConfig configures the golbal logger
type LoggerConfig struct {
	File  string
	Level string // TRACE, DEBUG, INFO, WARN, ERROR, FATAL
	Lang  int    // 0 en, 1 zh
}

// EmbedderConfig configures the text embedding provider.
type EmbedderConfig struct {
	// Provider selects the embedding implementation: "ali" (DashScope) or "zhipu".
	// Default: "ali".
	Provider string

	// APIKey is the API key for the embedding service.
	// If empty, it will be read from the environment variable specified by EnvKey.
	APIKey string

	// EnvKey is the environment variable name to read the API key from.
	// For "ali" provider, default is "DASHSCOPE_API_KEY".
	// For "zhipu" provider, default is "ZHIPUAI_API_KEY".
	EnvKey string

	// Dimension is the vector dimension output by this Embedder.
	// Default: 2048.
	Dimension int
}

// VectorStoreConfig configures the vector store (knowledge base / trait search).
type VectorStoreConfig struct {
	// DBPath is the file path to the SQLite database.
	// Default: "./brain.db".
	DBPath string
}

// ChatLLMConfig configures the LLM chat completion client.
type ChatLLMConfig struct {
	// APIKey is the API key for the LLM service.
	// If empty, it will be read from the environment variable specified by EnvKey.
	APIKey string

	// EnvKey is the environment variable name to read the API key from.
	// Default: "DEEPSEEK_API_KEY".
	EnvKey string

	// BaseURL is the API base URL.
	// Default: "https://api.deepseek.com/beta".
	BaseURL string

	// Model is the model name.
	// Default: "deepseek-v4-flash".
	Model string

	// MaxToolCallIterations is the maximum number of tool call iterations
	// in the streaming loop before forcing a direct answer.
	// Default: 9.
	MaxToolCallIterations int
}

// WebSearchConfig configures the web search provider.
type WebSearchConfig struct {
	// Provider selects the search implementation: "bocha" or "zhipu".
	// If empty, web search is disabled.
	Provider string

	// APIKey is the API key for the search service.
	// If empty, it will be read from the environment variable specified by EnvKey.
	APIKey string

	// EnvKey is the environment variable name to read the API key from.
	// For "bocha" provider, default is "BOCHA_API_KEY".
	// For "zhipu" provider, default is "ZHIPUAI_API_KEY".
	EnvKey string
}
