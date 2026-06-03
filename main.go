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
			DBPath: "./data/brain.db",
		},
		ChatLLM: config.ChatLLMConfig{
			EnvKey:                "DEEPSEEK_API_KEY",
			BaseURL:               "https://api.deepseek.com/beta",
			Model:                 "deepseek-v4-flash",
			MaxToolCallIterations: 2,
		},
		TraitLLM: config.TraitLLMConfig{
			EnvKey:                "DEEPSEEK_API_KEY",
			BaseURL:               "https://api.deepseek.com/beta",
			Model:                 "deepseek-v4-flash",
			MaxToolCallIterations: 3,
			ExtractInterval:       5,
			ExtractTokenThreshold: 200,
		},
		WebSearch: config.WebSearchConfig{
			Provider: os.Getenv("SEARCHER_PROVIDER"),
		},
	}

	if err := logger.CreateTheLogger(zylog.NameToLevel(cfg.Logger.Level), cfg.Logger.File, zylog.Language(cfg.Logger.Lang), cfg.Logger.CustomLevelNames); err != nil {
		log.Fatalf("create the logger fail. %v", err)
	}

	theLogger := logger.TheLogger()

	// first log
	theLogger.Infof("the logger is created with level. level %s. file %s", cfg.Logger.Level, cfg.Logger.File)

	// ============================================================
	// Ensure data directory exists for SQLite databases
	// ============================================================
	if err := os.MkdirAll("./data", 0755); err != nil {
		theLogger.Fatalf("failed to create data directory: %v", err)
	}

	// ============================================================
	// Initialize user store (separate user.db)
	// ============================================================
	userStore, err := store.NewUserStore("./data/users.db")
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
	mux.Handle("/api/chat", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			chatHandler.OnNewMessage(w, r)
		case http.MethodDelete:
			chatHandler.OnChatDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/info/llm/chat", http.HandlerFunc(chatHandler.OnGetLLMInfo))
	mux.Handle("/api/session", http.HandlerFunc(chatHandler.OnSession))
	mux.Handle("/api/chat/title", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			chatHandler.OnProposeChatTitle(w, r)
		case http.MethodPut:
			chatHandler.OnPutChatTitle(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/chat/list", http.HandlerFunc(chatHandler.OnGetChats))
	mux.Handle("/api/chat/new", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			chatHandler.OnNewChat(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/chat/pin", http.HandlerFunc(chatHandler.OnChatPin))
	mux.Handle("/api/chat/switch", http.HandlerFunc(chatHandler.OnSwitchChat))
	mux.Handle("/api/chat/messages", http.HandlerFunc(chatHandler.OnDeleteMessage))
	mux.Handle("/api/chat/login", http.HandlerFunc(chatHandler.OnLogin))
	mux.Handle("/api/chat/logout", http.HandlerFunc(chatHandler.OnLogout))

	// Recycle bin (trash) endpoints
	mux.Handle("/api/chat/deleted", http.HandlerFunc(chatHandler.OnListDeletedChats))
	mux.Handle("/api/chat/restore", http.HandlerFunc(chatHandler.OnRestoreChat))
	mux.Handle("/api/chat/permanent", http.HandlerFunc(chatHandler.OnPermanentDelete))
	mux.Handle("/api/chat/trash", http.HandlerFunc(chatHandler.OnEmptyTrash))

	// Health check endpoint
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"version": "1.0.0",
		})
	})

	// Static file server — frontend pages
	// Dev environment (DEV_CACHE_DISABLE=true) disables caching so frontend changes take effect immediately.
	// Production environment (default) uses http.FileServer's default ETag/Last-Modified caching behavior.
	fs := http.FileServer(http.Dir("./frontend"))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("DEV_CACHE_DISABLE") == "true" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		fs.ServeHTTP(w, r)
	}))

	// Start HTTP Server
	addr := "[::]:8080"
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

	theLogger.Infof("agent server listening on: http://%s", addr)
	theLogger.Infof("frontend: http://%s", addr)

	theLogger.Infof("press Ctrl+C to stop the server")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		theLogger.Fatalf("server failed to start: %v", err)
		return
	}

	// Wait for all cleanup to complete
	theLogger.Infof("Server shut down gracefully")
}
