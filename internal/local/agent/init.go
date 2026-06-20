package agent

import (
	"context"
	"fmt"
	"os"

	"BrainForever/infra/embedder"
	"BrainForever/infra/llm"
	"BrainForever/infra/searcher"
	"BrainForever/infra/zylog"
	"BrainForever/internal/local/agent/toolimp"
	"BrainForever/internal/local/config"
	"BrainForever/internal/local/store"
)

// ============================================================
// Agent initialization - creates the core objects
// from a unified Config struct.
// ============================================================

// InitEmbedder creates an Embedder based on the given configuration.
func InitEmbedder(cfg config.EmbedderConfig, logger zylog.Logger) embedder.Embedder {
	provider := cfg.Provider
	if provider == "" {
		provider = "ali"
	}

	dimension := cfg.Dimension
	if dimension <= 0 {
		dimension = 2048
	}

	var e embedder.Embedder
	switch provider {
	case "zhipu":
		envKey := cfg.EnvKey
		if envKey == "" {
			envKey = "ZHIPUAI_API_KEY"
		}
		e = embedder.NewZhipuEmbedder(cfg.APIKey, envKey, dimension)

		logger.Infof("✓ Using Zhipu Embedder: %s (%d dims)", e.Model(), e.Dimension())
	default:
		envKey := cfg.EnvKey
		if envKey == "" {
			envKey = "DASHSCOPE_API_KEY"
		}
		e = embedder.NewDashScopeEmbedder(cfg.APIKey, envKey, dimension)
		logger.Infof("✓ Using DashScope Embedder: %s (%d dims)", e.Model(), e.Dimension())
	}

	return e
}

// InitVectorStore creates a VectorStore (knowledge base / trait search).
// dimension is passed explicitly since embedding is now done externally.
func InitVectorStore(cfg config.VectorStoreConfig, dimension int, logger zylog.Logger) (*store.VectorStore, error) {
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "./localdb/brain.db"
	}

	vs, err := store.NewVectorStore(dbPath, dimension, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	logger.Infof("✓ VectorStore initialized: %s (dimension=%d)", dbPath, dimension)
	return vs, nil
}

// InitLLMClient creates an LLM chat completion client.
func InitLLMClient(cfg config.ChatLLMConfig, logger zylog.Logger) llm.Client {
	envKey := cfg.EnvKey
	if envKey == "" {
		envKey = "DEEPSEEK_API_KEY"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/beta"
	}

	model := cfg.Model
	if model == "" {
		model = "deepseek-v4-flash"
	}

	maxIter := cfg.MaxToolCallIterations
	if maxIter <= 0 {
		maxIter = 9
	}

	return llm.NewDeepSeekClientFromConfig(llm.DeepseekClientConfig{
		ClientConfig: llm.ClientConfig{
			EnvKey:                envKey,
			BaseURL:               baseURL,
			Model:                 model,
			MaxToolCallIterations: maxIter,
		},
	})
}

// InitWebSearchClient creates a web search client based on the given configuration.
// Returns nil if the provider is empty or the API key is not set.
func InitWebSearchClient(cfg config.WebSearchConfig, logger zylog.Logger) toolimp.WebSearcher {
	provider := cfg.Provider
	if provider == "" {
		provider = os.Getenv("SEARCHER_PROVIDER")
	}
	if provider == "" {
		return nil
	}

	switch provider {
	case "zhipu":
		envKey := cfg.EnvKey
		if envKey == "" {
			envKey = "ZHIPUAI_API_KEY"
		}
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv(envKey)
		}
		if apiKey != "" {
			logger.Infof("✓ Web search enabled (bigmodel.cn)")
			return &webSearchAdapter{
				client: searcher.NewZhiPuClient(searcher.WebSearchClientConfig{
					APIKey: apiKey,
				}),
			}
		}
		logger.Warnf("%s is not set or empty - web search will be disabled. "+
			"Set the %s environment variable to enable web search functionality.", envKey, envKey)

	case "bocha":
		envKey := cfg.EnvKey
		if envKey == "" {
			envKey = "BOCHA_API_KEY"
		}
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv(envKey)
		}
		if apiKey != "" {
			logger.Infof("✓ Web search enabled (bocha.cn)")
			return &webSearchAdapter{
				client: searcher.NewBochaClient(searcher.WebSearchClientConfig{
					APIKey: apiKey,
				}),
			}
		}
		logger.Warnf("%s is not set or empty - web search will be disabled. "+
			"Set the %s environment variable to enable web search functionality.", envKey, envKey)
	}

	return nil
}

// InitAgent creates a fully initialized ChatHandler from a Config.
// It also starts the background session GC.
// logger: the global logger instance for logging initialization progress.
func InitAgent(ctx context.Context, cfg config.Config, cookieName string, defaultLang string, logger zylog.Logger) (*ChatAgent, error) {
	// 1. Initialize Embedder
	embeddingClient := InitEmbedder(cfg.Embedder, logger)

	// 2. Initialize VectorStore
	vectorStore, err := InitVectorStore(cfg.VectorStore, embeddingClient.Dimension(), logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	// 3. Initialize Chat LLM Client
	chatLLMClient := InitLLMClient(cfg.ChatLLM, logger)

	// 4. Initialize Web Search Client
	webSearchClient := InitWebSearchClient(cfg.WebSearch, logger)

	// 5. Initialize anonymous user ChatStore (localdb/anonymous.chats.db)
	anonymousStore, err := store.CreateLocalChatScheme("localdb/anonymous.chats.db")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize anonymous chat store: %w", err)
	}

	// 6. Create ChatHandler
	chatHandler := NewChatHandler(
		&traitSearchAdapter{store: vectorStore, embedder: embeddingClient},
		webSearchClient,
		chatLLMClient,
		cookieName,
		defaultLang,
		anonymousStore,
		logger,
	)

	return chatHandler, nil
}
