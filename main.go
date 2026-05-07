package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"BrainOnline/infra/3rdapi/embedder"
	"BrainOnline/infra/3rdapi/llm_raw"
	"BrainOnline/infra/3rdapi/searcher"

	"BrainOnline/internal/agent"
	"BrainOnline/internal/store"
)

// ============================================================
// main
// ============================================================

func main() {
	dbPath := "./brain.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	// ============================================================
	// Initialize user store (separate user.db)
	// ============================================================
	userStore, err := store.NewUserStore("./users.db")
	if err != nil {
		log.Fatalf("failed to initialize user store: %v", err)
	}
	defer userStore.Close()
	fmt.Printf("✓ User store initialized (users.db)\n")

	// Select Embedder implementation via EMBEDDER_PROVIDER env var:
	//   "ali"   — Alibaba Tongyi text-embedding-v4 (2048 dims)
	//   "zhipu" — Zhipu GLM embedding-3 (2048 dims)
	embedderProvider := os.Getenv("EMBEDDER_PROVIDER")
	if embedderProvider == "" {
		embedderProvider = "ali"
	}

	var e embedder.Embedder
	switch embedderProvider {
	case "zhipu":
		// Zhipu embedding-3 fixed at 2048 dimensions
		e = embedder.NewZhipuEmbedder("", "ZHIPUAI_API_KEY", 2048)
		fmt.Printf("✓ Using Zhipu Embedder: %s (%d dims)\n", e.Model(), e.Dimension())
	default:
		// Alibaba text-embedding-v4 supports specifying dimension via API parameter (max 2048)
		// Set to 2048 to match Zhipu embedding-3, ensuring uniform dimensions in the same database
		e = embedder.NewDashScopeEmbedder("", "DASHSCOPE_API_KEY", 2048)
		fmt.Printf("✓ Using DashScope Embedder: %s (%d dims)\n", e.Model(), e.Dimension())
	}

	// Dimension obtained from Embedder to ensure consistency with vector index
	store, err := store.NewVectorStore(dbPath, e)
	if err != nil {
		log.Fatalf("Failed to initialize vector store: %v", err)
	}
	defer store.Close()

	// Initialize LLM AI client (raw HTTP client with tool call support)
	llmClient := llm_raw.NewDeepSeekRawFromConfig(llm_raw.RawClientConfig{
		EnvKey:  "DEEPSEEK_API_KEY",
		BaseURL: "https://api.deepseek.com/beta",
		Model:   "deepseek-v4-flash",
	})

	// Initialize Web Search client (optional — only if BOCHA_API_KEY is set)
	var webSearchClient agent.WebSearcher
	apiKey := os.Getenv("BOCHA_API_KEY")
	if apiKey != "" {
		webSearchClient = &webSearchAdapter{
			client: searcher.NewBochaClient(searcher.WebSearchClientConfig{
				APIKey: apiKey,
			}),
		}
		fmt.Println("✓ Web search enabled (bocha.ai)")
	} else {
		log.Printf("[WARN] BOCHA_API_KEY is not set or empty — web search will be disabled. " +
			"Set the BOCHA_API_KEY environment variable to enable web search functionality.")
	}

	// Create a signal-aware context: auto-cancels on SIGINT/SIGTERM
	// Declared early so StartGC can use it; the graceful shutdown goroutine
	// also uses this same ctx later.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Create Chat Handler (using adapters), pass cookie name for session management
	chatHandler := agent.NewChatHandler(&traitSearchAdapter{store: store}, webSearchClient, llmClient, "brain_go_session")

	// Start background session GC — cleans up idle sessions every hour
	// Uses the same context as graceful shutdown, so GC stops when the server exits
	chatHandler.StartGC(ctx)

	// Setup routes
	mux := http.NewServeMux()

	// API routes
	mux.Handle("/api/chat", http.HandlerFunc(chatHandler.OnNewMessage))
	mux.Handle("/api/session", http.HandlerFunc(chatHandler.OnRestoreSession))
	mux.Handle("/api/history", http.HandlerFunc(chatHandler.OnDeleteMessage))

	// Health check endpoint
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"version": "1.0.0",
		})
	})

	// Static file server — frontend pages
	fs := http.FileServer(http.Dir("./frontend"))
	mux.Handle("/", fs)

	// Start HTTP Server
	addr := ":8080"
	if envAddr := os.Getenv("PROXY_ADDR"); envAddr != "" {
		addr = envAddr
	}

	server := &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	// ============================================================
	// Graceful shutdown — use signal.NotifyContext for reliable exit
	// ============================================================

	go func() {
		<-ctx.Done()
		fmt.Println("\nShutting down server...")

		// Allow up to 10 seconds for in-flight requests to complete
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown timed out or errored: %v", err)
			// Force close underlying connections after timeout
			server.Close()
		}
	}()

	fmt.Printf("\n=== BrainOnline Agent Server ===\n")
	fmt.Printf("Listening on: http://%s\n", addr)
	fmt.Printf("API endpoint: http://%s/api/chat\n", addr)
	fmt.Printf("Frontend: http://%s\n", addr)
	fmt.Printf("Health check: http://%s/api/health\n", addr)
	fmt.Println("Press Ctrl+C to stop the server")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed to start: %v", err)
	}

	// Wait for all cleanup to complete
	fmt.Println("Server shut down gracefully")
}
