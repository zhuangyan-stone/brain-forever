package agent

import (
	"context"
	"fmt"
	"log"
	"os"

	"BrainOnline/infra/embedder"
	"BrainOnline/infra/llm"
	"BrainOnline/infra/searcher"
	"BrainOnline/internal/agent/toolimp"
	"BrainOnline/internal/config"
	"BrainOnline/internal/store"
)

// ============================================================
// Agent initialization — creates the four core objects
// from a unified Config struct.
// ============================================================

// InitEmbedder creates an Embedder based on the given configuration.
func InitEmbedder(cfg config.EmbedderConfig) embedder.Embedder {
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
		fmt.Printf("✓ Using Zhipu Embedder: %s (%d dims)\n", e.Model(), e.Dimension())
	default:
		envKey := cfg.EnvKey
		if envKey == "" {
			envKey = "DASHSCOPE_API_KEY"
		}
		e = embedder.NewDashScopeEmbedder(cfg.APIKey, envKey, dimension)
		fmt.Printf("✓ Using DashScope Embedder: %s (%d dims)\n", e.Model(), e.Dimension())
	}

	return e
}

// InitVectorStore creates a VectorStore (knowledge base / trait search).
func InitVectorStore(cfg config.VectorStoreConfig, e embedder.Embedder) (*store.VectorStore, error) {
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "./brain.db"
	}

	vs, err := store.NewVectorStore(dbPath, e)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	return vs, nil
}

// InitLLMClient creates an LLM chat completion client.
func InitLLMClient(cfg config.ChatLLMConfig) llm.LLMClient {
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
func InitWebSearchClient(cfg config.WebSearchConfig) toolimp.WebSearcher {
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
			fmt.Println("✓ Web search enabled (bigmodel.cn)")
			return &webSearchAdapter{
				client: searcher.NewZhiPuClient(searcher.WebSearchClientConfig{
					APIKey: apiKey,
				}),
			}
		}
		log.Printf("[WARN] %s is not set or empty — web search will be disabled. "+
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
			fmt.Println("✓ Web search enabled (bocha.cn)")
			return &webSearchAdapter{
				client: searcher.NewBochaClient(searcher.WebSearchClientConfig{
					APIKey: apiKey,
				}),
			}
		}
		log.Printf("[WARN] %s is not set or empty — web search will be disabled. "+
			"Set the %s environment variable to enable web search functionality.", envKey, envKey)
	}

	return nil
}

// InitAgent creates a fully initialized ChatHandler from a Config.
// It also starts the background session GC.
func InitAgent(ctx context.Context, cfg config.Config, cookieName string, defaultLang string) (*ChatAgent, error) {
	// 1. Initialize Embedder
	embeddingClient := InitEmbedder(cfg.Embedder)

	// 2. Initialize VectorStore
	vectorStore, err := InitVectorStore(cfg.VectorStore, embeddingClient)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	// 3. Initialize LLM Client
	chatLLMClient := InitLLMClient(cfg.ChatLLM)

	// 4. Initialize Web Search Client
	webSearchClient := InitWebSearchClient(cfg.WebSearch)

	// 5. Create ChatHandler
	chatHandler := NewChatHandler(
		&traitSearchAdapter{store: vectorStore},
		webSearchClient,
		chatLLMClient,
		cookieName,
		defaultLang,
	)

	// 6. Start background session GC
	chatHandler.StartGC(ctx)

	return chatHandler, nil
}
