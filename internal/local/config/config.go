package config

// ============================================================
// Config -centralized configuration for the BrainForever agent
//
// This struct holds the configuration for the core objects
// that are initialized in agent/init.go:
//   - Embedder (text embedding)
//   - LLMClient (chat completion)
//   - WebSearchClient (online search)
// ============================================================

// Config is the top-level configuration for the agent layer.
type Config struct {
	Logger    LoggerConfig
	Server    ServerConfig
	Frontend  FrontendConfig
	Embedder  EmbedderConfig
	ChatLLM   ChatLLMConfig
	WebSearch WebSearchConfig
}

// ServerConfig configures the HTTP server.
type ServerConfig struct {
	// Name is the server identifier, used in logs.
	Name string

	// Addr is the listen address, e.g. ":8080" or "[::]:8080".
	// Overridable by PROXY_ADDR environment variable.
	Addr string

	// ReadTimeout is the maximum duration for reading the entire request, including the body.
	ReadTimeout int // seconds, 0 = no timeout
	// ReadHeaderTimeout is the amount of time allowed to read request headers.
	ReadHeaderTimeout int // seconds, 0 = no timeout
	// WriteTimeout is the maximum duration before timing out writes of the response.
	WriteTimeout int // seconds, 0 = no timeout
	// IdleTimeout is the maximum amount of time to wait for the next request when keep-alives are enabled.
	IdleTimeout int // seconds, 0 = no timeout
}

// FrontendConfig configures the static file serving for the frontend.
type FrontendConfig struct {
	// Dir is the directory path for frontend static files.
	// Default: "./frontend".
	Dir string

	// CacheDisable disables browser caching for development.
	// When true, sets Cache-Control: no-cache headers on static files.
	CacheDisable bool
}

// LoggerConfig configures the golbal logger
type LoggerConfig struct {
	File             string
	Level            string   // TRACE, DEBUG, INFO, WARN, ERROR, FATAL
	Lang             int      // 0 en, 1 custom
	CustomLevelNames []string // Custom level names for LanguageCustom, e.g. {"TRACE","DEBUG","INFO","WARN","ERROR","FATAL","OFF"}
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
