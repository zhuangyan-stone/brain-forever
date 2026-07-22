package agent

import (
	"context"

	"BrainForever/infra/embedder"
	"BrainForever/infra/llm"
	"BrainForever/infra/searcher"
	"BrainForever/infra/zylog"
	"BrainForever/internal/config"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
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
// ============================================================

var (
	llmClients          map[string]llm.Client
	embedderClients     map[string]embedder.Embedder
	searcherClientByPvd map[string]searcher.WebSearcher

	// Global store instances (shared, no per-user file management)
	theChatStore  *store.ChatStore
	theBrainStore *store.BrainStore
)

// ============================================================
// Agent initialization
// ============================================================

func InitEmbedder(provider string, dimension int, logger zylog.Logger) embedder.Embedder {
	if provider == "" {
		provider = ProviderAli
	}
	if dimension <= 0 {
		dimension = 1024 // NOTE: limit by pg-vector
	}

	var e embedder.Embedder
	switch provider {
	case ProviderZhipu:
		e = embedder.NewZhipuEmbedder("", dimension)
		logger.Infof("- Using Zhipu Embedder: %s (%d dims)", e.Model(), e.Dimension())
	default:
		e = embedder.NewDashScopeEmbedder("", dimension)
		logger.Infof("- Using DashScope Embedder: %s (%d dims)", e.Model(), e.Dimension())
	}

	return e
}

func InitLLMClient(logger zylog.Logger) llm.Client {
	logger.Infof("- Using DeepSeek LLM client")
	return llm.NewDeepSeekClientFromConfig(llm.DeepseekClientConfig{})
}

func InitWebSearchRawClient(provider string, logger zylog.Logger) searcher.WebSearcher {
	switch provider {
	case ProviderZhipu:
		logger.Infof("- Web search client registered (bigmodel.cn)")
		return searcher.NewZhiPuClient(searcher.WebSearchClientConfig{})

	case ProviderBocha:
		logger.Infof("- Web search client registered (bocha.cn)")
		return searcher.NewBochaClient(searcher.WebSearchClientConfig{})
	}

	return nil
}

func InitRedisStore(cfg config.RedisConfig) *cache.RedisSessionStore {
	if cfg.Addr == "" {
		return nil
	}
	return cache.NewRedisSessionStore(&cfg)
}

// InitAgent creates a fully initialized ChatHandler from a Config.
func InitAgent(ctx context.Context, cfg config.Config, cookieName string, defaultLang string, logger zylog.Logger) (*ChatAgent, error) {
	llmClients = make(map[string]llm.Client)
	llmClients[ProviderDeepSeek] = InitLLMClient(logger)

	const embedderDimension = 1024
	embedderClients = make(map[string]embedder.Embedder)
	embedderClients[ProviderAli] = InitEmbedder(ProviderAli, embedderDimension, logger)
	embedderClients[ProviderZhipu] = InitEmbedder(ProviderZhipu, embedderDimension, logger)

	searcherClientByPvd = make(map[string]searcher.WebSearcher)
	searcherClientByPvd[ProviderBocha] = InitWebSearchRawClient(ProviderBocha, logger)
	searcherClientByPvd[ProviderZhipu] = InitWebSearchRawClient(ProviderZhipu, logger)

	// Create global store instances (single shared connection pool via ThePGDB())
	theChatStore = store.NewChatStore(logger)
	theBrainStore = store.NewBrainStore(logger)

	avatarDir := cfg.Frontend.Dir + "/static/img/avatar"

	gcCfg := session.FromTOMLConfig(session.SessionGCConfigTOML{
		AnonymousTTLMinutes: cfg.SessionGCTask.AnonymousTTLMinutes,
		LoggedInTTLMinutes:  cfg.SessionGCTask.LoggedInTTLMinutes,
		IntervalMinutes:     cfg.SessionGCTask.IntervalMinutes,
	})

	chatHandler := NewChatHandler(
		cookieName,
		defaultLang,
		avatarDir,
		logger,
		gcCfg,
		cfg.TraitTask.DeduplicateEnabled,
		cfg.TraitTask.DeduplicateThreshold,
	)

	redisStore := InitRedisStore(cfg.Redis)
	if redisStore != nil {
		if err := redisStore.Ping(ctx); err != nil {
			logger.Fatalf("Redis is configured but unreachable. %v", err)
		}

		chatHandler.SetRedisStore(redisStore)
		logger.Infof("- Redis session store attached (%s)", cfg.Redis.Addr)

		smsCodeCache := cache.NewSMSCodeCache(redisStore.Client())
		chatHandler.SetSMSCodeCache(smsCodeCache)
		logger.Infof("- SMS code cache attached (Redis-based)")
	} else {
		logger.Fatalf("Redis not configured")
	}

	return chatHandler, nil
}
