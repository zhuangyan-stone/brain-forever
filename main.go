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
	"BrainForever/infra/zylog"
	"BrainForever/internal/agent"
	"BrainForever/internal/config"
	"BrainForever/internal/logger"
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
		Logger: config.LoggerConfig{
			File:  "log/brain-forever.log",
			Level: "TRACE",
			Lang:  0,
		},

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

	if err := logger.CreateTheLogger(zylog.NameToLevel(cfg.Logger.Level), cfg.Logger.File, zylog.Language(cfg.Logger.Lang)); err != nil {
		log.Fatalf("create the logger fail. %v", err)
	}

	theLogger := logger.TheLogger()

	// first log
	theLogger.Infof("the logger is created with level. level %s. file %s", cfg.Logger.Level, cfg.Logger.File)

	// ============================================================
	// Initialize user store (separate user.db)
	// ============================================================
	userStore, err := store.NewUserStore("./users.db")
	if err != nil {
		theLogger.Fatalf("failed to initialize user store: %v", err)
	} else {
		theLogger.Infof("users storage created.")
	}
	defer userStore.Close()

	// ============================================================
	// Determine default language for i18n from environment variable
	// ============================================================
	defaultLang := os.Getenv("DEFAULT_LANG")
	if defaultLang == "" {
		defaultLang = "zh-CN" // Default to Chinese for Chinese users
	}

	i18n.SetDefaultLanguage(defaultLang)
	theLogger.Infof("default lang set to %s", defaultLang)

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
		theLogger.Fatalf("failed to initialize agent: %v", err)
		return
	}
	defer chatHandler.Close()
	theLogger.Infof("AI agent (Brain-Forever) is now active")

	// ============================================================
	// Setup routes
	// ============================================================
	mux := http.NewServeMux()

	// API routes
	mux.Handle("/api/chat", http.HandlerFunc(chatHandler.OnNewMessage))
	mux.Handle("/api/session", http.HandlerFunc(chatHandler.OnRestoreSession))
	mux.Handle("/api/session/new", http.HandlerFunc(chatHandler.OnNewSession))
	mux.Handle("/api/session/title", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			chatHandler.OnGetSessionTitle(w, r)
		case http.MethodPut:
			chatHandler.OnPutSessionTitle(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
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
	addr := "0.0.0.0:8080"
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

	theLogger.Infof("aget server listening on: http://%s", addr)
	theLogger.Infof("frontend: http://%s", addr)

	theLogger.Infof("press Ctrl+C to stop the server")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		theLogger.Fatalf("server failed to start: %v", err)
		return
	}

	// Wait for all cleanup to complete
	theLogger.Infof("Server shut down gracefully")
}
