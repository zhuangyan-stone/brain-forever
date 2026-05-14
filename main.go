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

	"BrainForever/infra/httpx"
	"BrainForever/infra/i18n"
	"BrainForever/internal/agent"
	"BrainForever/internal/config"
	"BrainForever/internal/store"
)

// ============================================================
// main
// ============================================================

func main() {
	// ============================================================
	// Build configuration from environment variables
	// ============================================================

	cfg := config.Config{
		Embedder: config.EmbedderConfig{
			Provider:  os.Getenv("EMBEDDER_PROVIDER"),
			Dimension: 2048,
		},
		VectorStore: config.VectorStoreConfig{
			DBPath: "./brain.db",
		},
		ChatLLM: config.ChatLLMConfig{
			EnvKey:                "DEEPSEEK_API_KEY",
			BaseURL:               "https://api.deepseek.com/beta",
			Model:                 "deepseek-v4-flash",
			MaxToolCallIterations: 9,
		},
		WebSearch: config.WebSearchConfig{
			Provider: os.Getenv("SEARCHER_PROVIDER"),
		},
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

	// ============================================================
	// Determine default language for i18n from environment variable
	// ============================================================
	defaultLang := os.Getenv("DEFAULT_LANG")
	if defaultLang == "" {
		defaultLang = "zh-CN" // Default to Chinese for Chinese users
	}
	i18n.SetDefaultLanguage(defaultLang)

	// ============================================================
	// Create a signal-aware context: auto-cancels on SIGINT/SIGTERM
	// ============================================================
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ============================================================
	// Initialize agent (Embedder, VectorStore, LLMClient, WebSearchClient)
	// ============================================================
	chatHandler, err := agent.InitAgent(ctx, cfg, "brain_go_session", defaultLang)
	if err != nil {
		log.Fatalf("failed to initialize agent: %v", err)
		return
	}
	defer chatHandler.Close()

	// ============================================================
	// Setup routes
	// ============================================================
	mux := http.NewServeMux()

	// API routes
	mux.Handle("/api/chat", http.HandlerFunc(chatHandler.OnNewMessage))
	mux.Handle("/api/session", http.HandlerFunc(chatHandler.OnRestoreSession))
	mux.Handle("/api/session/new", http.HandlerFunc(chatHandler.OnNewSession))
	mux.Handle("/api/session/title", http.HandlerFunc(chatHandler.OnGetSessionTitle))
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
		Handler: httpx.UseCORSMiddleware(mux),
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

	fmt.Printf("\n=== 脑力永恒 Agent Server ===\n")
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
