package agent

import (
	"context"

	"BrainForever/infra/embedder"
	"BrainForever/infra/llm"
	"BrainForever/infra/searcher"
	"BrainForever/infra/zylog"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/config"
	"BrainForever/internal/store/cache"
	"BrainForever/internal/store/dbc"
)

// ============================================================
// Provider constants
// ============================================================

const (
	// ProviderDeepSeek is the default LLM provider.
	ProviderDeepSeek = "deepseek"
	// ProviderAli is the DashScope (Alibaba Cloud) embedder provider.
	ProviderAli = "ali"
	// ProviderZhipu is the Zhipu AI provider (embedder + searcher).
	ProviderZhipu = "zhipu"
	// ProviderBocha is the Bocha web search provider.
	ProviderBocha = "bocha"
)

// ============================================================
// Provider client registries (package-level singletons)
//
// These maps hold pre-configured API clients for each provider,
// initialized once during InitAgent. All ChatAgent instances
// share the same registries and underlying connection pools.
// ============================================================

var (
	llmClients       map[string]llm.Client          // provider -> LLM client
	embedderClients  map[string]embedder.Embedder   // provider -> Embedder
	webSearchClients map[string]toolimp.WebSearcher // provider -> WebSearcher
)

// ============================================================
// Agent initialization - creates the core objects
// from a unified Config struct.
// ============================================================

// InitEmbedder creates an Embedder based on the given configuration.
func InitEmbedder(cfg config.EmbedderConfig, logger zylog.Logger) embedder.Embedder {
	provider := cfg.Provider
	if provider == "" {
		provider = ProviderAli
	}

	dimension := cfg.Dimension
	if dimension <= 0 {
		dimension = 2048
	}

	var e embedder.Embedder
	switch provider {
	case ProviderZhipu:
		e = embedder.NewZhipuEmbedder(cfg.APIKey, dimension)
		logger.Infof("? Using Zhipu Embedder: %s (%d dims)", e.Model(), e.Dimension())
	default:
		e = embedder.NewDashScopeEmbedder(cfg.APIKey, dimension)
		logger.Infof("? Using DashScope Embedder: %s (%d dims)", e.Model(), e.Dimension())
	}

	return e
}

// InitLLMClient creates an LLM chat completion client.
// The API key comes from cfg.APIKey (which reads from config file/env).
// If empty, the client has no default key — callers must provide one per-request.
func InitLLMClient(cfg config.ChatLLMConfig, logger zylog.Logger) llm.Client {
	return llm.NewDeepSeekClientFromConfig(llm.DeepseekClientConfig{
		ClientConfig: llm.ClientConfig{
			APIKey:                cfg.APIKey,
			BaseURL:               cfg.BaseURL,
			Model:                 cfg.Model,
			MaxToolCallIterations: cfg.MaxToolCallIterations,
		},
	})
}

// InitWebSearchClient creates a web search client based on the given configuration.
// Returns nil if the provider is empty or not recognized.
func InitWebSearchClient(cfg config.WebSearchConfig, logger zylog.Logger) toolimp.WebSearcher {
	provider := cfg.Provider
	if provider == "" {
		return nil
	}

	switch provider {
	case ProviderZhipu:
		logger.Infof("? Web search enabled (bigmodel.cn)")
		return &webSearchAdapter{
			client: searcher.NewZhiPuClient(searcher.WebSearchClientConfig{
				APIKey: cfg.APIKey,
			}),
		}

	case ProviderBocha:
		logger.Infof("? Web search enabled (bocha.cn)")
		return &webSearchAdapter{
			client: searcher.NewBochaClient(searcher.WebSearchClientConfig{
				APIKey: cfg.APIKey,
			}),
		}
	}

	return nil
}

// InitRedisStore creates a Redis session store from config.
// Returns nil if Redis configuration is empty (Redis optional).
func InitRedisStore(cfg config.RedisConfig) *cache.RedisSessionStore {
	if cfg.Addr == "" {
		return nil
	}
	return cache.NewRedisSessionStore(cfg.Addr, cfg.Password, cfg.DB)
}

// InitAgent creates a fully initialized ChatHandler from a Config.
// It also starts the background session GC.
// logger: the global logger instance for logging initialization progress.
func InitAgent(ctx context.Context, cfg config.Config, cookieName string, defaultLang string, logger zylog.Logger) (*ChatAgent, error) {
	// 1. Initialize LLM client registry (package-level singleton)
	llmClients = make(map[string]llm.Client)
	llmClients[ProviderDeepSeek] = InitLLMClient(cfg.ChatLLM, logger)

	// 2. Initialize Embedder registry (package-level singleton)
	embedderClients = make(map[string]embedder.Embedder)
	embedderClients[ProviderAli] = InitEmbedder(config.EmbedderConfig{
		Provider:  ProviderAli,
		APIKey:    cfg.Embedder.APIKey,
		Dimension: cfg.Embedder.Dimension,
	}, logger)
	embedderClients[ProviderZhipu] = InitEmbedder(config.EmbedderConfig{
		Provider:  ProviderZhipu,
		APIKey:    cfg.Embedder.APIKey,
		Dimension: cfg.Embedder.Dimension,
	}, logger)

	// 3. Initialize Web Search registry (package-level singleton)
	webSearchClients = make(map[string]toolimp.WebSearcher)
	webSearchClients[ProviderBocha] = InitWebSearchClient(cfg.WebSearch, logger)
	webSearchClients[ProviderZhipu] = InitWebSearchClient(cfg.WebSearch, logger)

	// 4. Initialize dbc (global on-demand database manager)
	// Use the default embedder's dimension for vector index initialization
	defaultEmbedder := embedderClients[ProviderAli]
	dbc.InitDBConfig("localdb", defaultEmbedder.Dimension(), logger)

	// 5. Determine the avatar directory path (relative to the frontend directory)
	avatarDir := cfg.Frontend.Dir + "/static/img/avatar"

	// 6. Create ChatHandler (no longer needs client arguments)
	chatHandler := NewChatHandler(
		cookieName,
		defaultLang,
		avatarDir,
		logger,
	)

	// 7. Attach Redis session store (if configured)
	redisStore := InitRedisStore(cfg.Redis)
	if redisStore != nil {
		// Verify Redis connectivity
		if err := redisStore.Ping(ctx); err != nil {
			logger.Fatalf("Redis is configured but unreachable: %v", err)
		} else {
			chatHandler.SetRedisStore(redisStore)
			logger.Infof("? Redis session store attached (%s)", cfg.Redis.Addr)

			// Create SMS code cache using the same Redis connection pool
			smsCodeCache := cache.NewSMSCodeCache(redisStore.Client())
			chatHandler.SetSMSCodeCache(smsCodeCache)
			logger.Infof("? SMS code cache attached (Redis-based)")
		}
	}

	return chatHandler, nil
}
