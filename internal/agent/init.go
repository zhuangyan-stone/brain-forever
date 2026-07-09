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
	ProviderDeepSeek = "deepseek"
	ProviderAli      = "ali"
	ProviderZhipu    = "zhipu"
	ProviderBocha    = "bocha"
)

// ============================================================
// Provider client registries (package-level singletons)
// ============================================================

var (
	llmClients       map[string]llm.Client
	embedderClients  map[string]embedder.Embedder
	webSearchClients map[string]toolimp.WebSearcher
)

// ============================================================
// Agent initialization
// ============================================================

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

func InitRedisStore(cfg config.RedisConfig) *cache.RedisSessionStore {
	if cfg.Addr == "" {
		return nil
	}
	return cache.NewRedisSessionStore(cfg.Addr, cfg.Password, cfg.DB)
}

// InitAgent creates a fully initialized ChatHandler from a Config.
func InitAgent(ctx context.Context, cfg config.Config, cookieName string, defaultLang string, logger zylog.Logger) (*ChatAgent, error) {
	llmClients = make(map[string]llm.Client)
	llmClients[ProviderDeepSeek] = InitLLMClient(cfg.ChatLLM, logger)

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

	webSearchClients = make(map[string]toolimp.WebSearcher)
	webSearchClients[ProviderBocha] = InitWebSearchClient(cfg.WebSearch, logger)
	webSearchClients[ProviderZhipu] = InitWebSearchClient(cfg.WebSearch, logger)

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
