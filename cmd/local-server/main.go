package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"BrainForever/infra/httpx"
	"BrainForever/infra/i18n"
	"BrainForever/infra/zylog"
	"BrainForever/internal/local/agent"
	"BrainForever/internal/local/config"
	"BrainForever/internal/local/logger"
	"BrainForever/internal/local/store"
)

// ============================================================
// main
// ============================================================

func main() {
	// ============================================================
	// Build configuration from environment variables
	// ============================================================

	cfg := config.Config{
		Server: config.ServerConfig{
			Name:              "local-server",
			Addr:              "[::]:8080",
			ReadTimeout:       30,
			ReadHeaderTimeout: 10,
			WriteTimeout:      0, // 0 = disabled -SSE streaming requires long-lived connections
			IdleTimeout:       60,
		},
		Frontend: config.FrontendConfig{
			Dir:          "./frontend",
			CacheDisable: false,
		},
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
			DBPath: "./localdb/brain.db",
		},
		ChatLLM: config.ChatLLMConfig{
			EnvKey:                "DEEPSEEK_API_KEY",
			BaseURL:               "https://api.deepseek.com/beta",
			Model:                 "deepseek-v4-flash",
			MaxToolCallIterations: 20,
		},
		WebSearch: config.WebSearchConfig{
			Provider: os.Getenv("SEARCHER_PROVIDER"),
		},
	}

	// Allow env var overrides for server address
	if envAddr := os.Getenv("PROXY_ADDR"); envAddr != "" {
		cfg.Server.Addr = envAddr
	}
	if envDisable := os.Getenv("DEV_CACHE_DISABLE"); envDisable == "true" {
		cfg.Frontend.CacheDisable = true
	}

	if err := logger.CreateTheLogger(zylog.NameToLevel(cfg.Logger.Level), cfg.Logger.File, zylog.Language(cfg.Logger.Lang), cfg.Logger.CustomLevelNames); err != nil {
		log.Fatalf("create the logger fail. %v", err)
	}

	theLogger := logger.TheLogger()

	// first log
	theLogger.Infof("the logger is created with level. level %s. file %s", cfg.Logger.Level, cfg.Logger.File)

	// ============================================================
	// Ensure localdb directory exists for SQLite databases
	// ============================================================
	if err := os.MkdirAll("./localdb", 0755); err != nil {
		theLogger.Fatalf("failed to create localdb directory: %v", err)
	}

	// ============================================================
	// Initialize user store (separate user.db)
	// ============================================================
	userStore, err := store.NewUserStore("./localdb/users.db")
	if err != nil {
		theLogger.Fatalf("failed to initialize user store: %v", err)
	} else {
		theLogger.Infof("users storage created.")
	}
	defer userStore.Close()

	// ============================================================
	// Initialize i18n with local language resources
	// ============================================================
	i18n.Init("lang/local")

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
	chatHandler, err := agent.InitAgent(ctx, cfg, "brain_go_session", defaultLang, theLogger)
	if err != nil {
		theLogger.Fatalf("failed to initialize agent: %v", err)
		return
	}
	defer chatHandler.Close()
	theLogger.Infof("AI agent (Brain-Forever) is now active")

	// ============================================================
	// Setup routes using httpx.Server
	// ============================================================

	// Parse server address into host and port
	host := "[::]"
	port := uint16(8080)
	if h, p, err := net.SplitHostPort(cfg.Server.Addr); err == nil {
		host = h
		if pn, err := strconv.ParseUint(p, 10, 16); err == nil {
			port = uint16(pn)
		}
	}

	srv := httpx.NewServer(httpx.Config{
		Name:              cfg.Server.Name,
		Host:              host,
		Port:              port,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeout) * time.Second,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(cfg.Server.IdleTimeout) * time.Second,
		Charset:           "utf-8",
	}, theLogger)

	// CORS middleware -support for frontend-backend separated development
	srv.Use(httpx.UseCORSMiddleware)

	srv.GET("/api/info/llm/chat", chatHandler.OnGetLLMInfo)
	srv.GET("/api/session", chatHandler.OnSession)
	srv.GET("/api/chat/list", chatHandler.OnGetChats)
	srv.PUT("/api/chat/new", chatHandler.OnNewChat)
	srv.PUT("/api/chat/pin", chatHandler.OnChatPin)
	srv.GET("/api/chat/switch", chatHandler.OnSwitchChat)
	srv.DELETE("/api/chat/messages", chatHandler.OnDeleteMessage)
	srv.POST("/api/chat/login", chatHandler.OnLogin)
	srv.POST("/api/chat/logout", chatHandler.OnLogout)

	// Recycle bin (trash) endpoints
	srv.GET("/api/chat/deleted", chatHandler.OnListDeletedChats)
	srv.PUT("/api/chat/restore", chatHandler.OnRestoreChat)
	srv.DELETE("/api/chat/permanent", chatHandler.OnPermanentDelete)
	srv.DELETE("/api/chat/trash", chatHandler.OnEmptyTrash)

	// Health check endpoint
	srv.GET("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"server":  "local-server",
			"version": "1.0.0",
		})
	})

	// /api/chat -POST (new message) + DELETE (delete chat)
	srv.POST("/api/chat", chatHandler.OnNewMessage)
	srv.DELETE("/api/chat", chatHandler.OnChatDelete)

	// /api/chat/title -GET (propose title) + PUT (save title)
	srv.GET("/api/chat/title", chatHandler.GetSuggestedChatTitle)
	srv.PUT("/api/chat/title", chatHandler.OnPutChatTitle)

	// /api/chat/traits -POST (extract personal traits via remote-server)
	srv.POST("/api/chat/traits", chatHandler.OnExtractTraits)

	// ── Static file server -frontend pages ──
	// When CacheDisable is true, sets Cache-Control: no-cache headers so frontend changes
	// take effect immediately during development.
	// Production (default) uses http.FileServer's default ETag/Last-Modified caching behavior.
	fs := http.FileServer(http.Dir(cfg.Frontend.Dir))
	srv.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Frontend.CacheDisable {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		fs.ServeHTTP(w, r)
	})

	// ============================================================
	// Start server & wait for shutdown signal
	// ============================================================

	srv.Start()
	theLogger.Infof("frontend: http://%s", srv.Addr())
	theLogger.Infof("press Ctrl+C to stop the server")

	<-ctx.Done()
	theLogger.Info("Shutting down server...")
	srv.Stop("received shutdown signal")
	theLogger.Info("Server shut down gracefully")
}
