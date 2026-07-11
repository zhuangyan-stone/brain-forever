package agent

import (
	"context"

	"BrainForever/infra/embedder"
	"BrainForever/infra/llm"
	"BrainForever/infra/searcher"
	"BrainForever/infra/zylog"
	"BrainForever/internal/config"
	"BrainForever/internal/store/cache"
	"BrainForever/internal/store/dbc"
)

// ============================================================
// Provider constants
// ============================================================

const (
	ProviderDeepSeek = "deepseek"
	ProviderAli      = "ali"
	ProviderZhipu    = "zhipu"
	ProviderBocha    = "bocha"
)

// ============================================================
// Provider client registries (package-level singletons)
//
// All clients are initialized with empty API keys. Per-user API
// keys are provided at runtime via session LLM/Embedder/Search
// API key accessors.
// ============================================================

var (
	llmClients          map[string]llm.Client
	embedderClients     map[string]embedder.Embedder
	searcherClientByPvd map[string]searcher.WebSearcher // underlying search clients (no API key)
)

// ============================================================
// Agent initialization
// ============================================================

// InitEmbedder creates an embedder for the given provider.
// The embedder is created with an empty API key; callers must
// provide the per-user API key at embed-time.
func InitEmbedder(provider string, dimension int, logger zylog.Logger) embedder.Embedder {
	if provider == "" {
		provider = ProviderAli
	}
	if dimension <= 0 {
		dimension = 2048
	}

	var e embedder.Embedder
	switch provider {
	case ProviderZhipu:
		e = embedder.NewZhipuEmbedder("", dimension)
		logger.Infof("? Using Zhipu Embedder: %s (%d dims)", e.Model(), e.Dimension())
	default:
		e = embedder.NewDashScopeEmbedder("", dimension)
		logger.Infof("? Using DashScope Embedder: %s (%d dims)", e.Model(), e.Dimension())
	}

	return e
}

// InitLLMClient creates the default DeepSeek LLM client.
// The client is created with an empty API key; callers must
// provide the per-user API key at call time.
func InitLLMClient(logger zylog.Logger) llm.Client {
	logger.Infof("? Using DeepSeek LLM client")
	return llm.NewDeepSeekClientFromConfig(llm.DeepseekClientConfig{})
}

// InitWebSearchRawClient creates a raw search client for the given provider.
// The client is created with an empty API key; callers must provide the
// per-user API key via webSearchAdapter at call time.
func InitWebSearchRawClient(provider string, logger zylog.Logger) searcher.WebSearcher {
	switch provider {
	case ProviderZhipu:
		logger.Infof("? Web search client registered (bigmodel.cn)")
		return searcher.NewZhiPuClient(searcher.WebSearchClientConfig{})

	case ProviderBocha:
		logger.Infof("? Web search client registered (bocha.cn)")
		return searcher.NewBochaClient(searcher.WebSearchClientConfig{})
	}

	return nil
}

func InitRedisStore(cfg config.RedisConfig) *cache.RedisSessionStore {
	if cfg.Addr == "" {
		return nil
	}
	return cache.NewRedisSessionStore(cfg.Addr, cfg.Password, cfg.DB)
}

// InitAgent creates a fully initialized ChatHandler from a Config.
func InitAgent(ctx context.Context, cfg config.Config, cookieName string, defaultLang string, logger zylog.Logger) (*ChatAgent, error) {
	llmClients = make(map[string]llm.Client)
	llmClients[ProviderDeepSeek] = InitLLMClient(logger)

	const embedderDimension = 2048
	embedderClients = make(map[string]embedder.Embedder)
	embedderClients[ProviderAli] = InitEmbedder(ProviderAli, embedderDimension, logger)
	embedderClients[ProviderZhipu] = InitEmbedder(ProviderZhipu, embedderDimension, logger)

	searcherClientByPvd = make(map[string]searcher.WebSearcher)
	searcherClientByPvd[ProviderBocha] = InitWebSearchRawClient(ProviderBocha, logger)
	searcherClientByPvd[ProviderZhipu] = InitWebSearchRawClient(ProviderZhipu, logger)

	defaultEmbedder := embedderClients[ProviderAli]
	dbc.InitDBConfig(cfg.Data.Dir, defaultEmbedder.Dimension(), logger)

	avatarDir := cfg.Frontend.Dir + "/static/img/avatar"

	chatHandler := NewChatHandler(
		cookieName,
		defaultLang,
		avatarDir,
		logger,
	)

	redisStore := InitRedisStore(cfg.Redis)
	if redisStore != nil {
		if err := redisStore.Ping(ctx); err != nil {
			logger.Fatalf("Redis is configured but unreachable: %v", err)
		}

		chatHandler.SetRedisStore(redisStore)
		logger.Infof("? Redis session store attached (%s)", cfg.Redis.Addr)

		smsCodeCache := cache.NewSMSCodeCache(redisStore.Client())
		chatHandler.SetSMSCodeCache(smsCodeCache)
		logger.Infof("? SMS code cache attached (Redis-based)")
	} else {
		logger.Fatalf("Redis not configured")
	}

	return chatHandler, nil
}
