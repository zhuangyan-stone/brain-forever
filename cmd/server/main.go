package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"BrainForever/infra/httpx"
	"BrainForever/infra/i18n"
	"BrainForever/infra/zylog"
	"BrainForever/internal/agent"
	"BrainForever/internal/config"
	"BrainForever/internal/logger"
	"BrainForever/internal/store"
	"BrainForever/internal/theme"
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
	// Initialize global UserStore singleton (localdb/users.db)
	// Opens before HTTP server starts, closes after it stops
	// ============================================================
	if err := store.InitTheUserStore("./localdb"); err != nil {
		theLogger.Fatalf("failed to initialize user store: %v", err)
	}
	defer store.CloseTheUserStore()
	theLogger.Infof("user store (users.db) initialized")

	// ============================================================
	// Initialize i18n with local language resources
	// ============================================================
	i18n.Init("lang")

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

	// Initialize theme handler
	themeHandler := theme.NewHandler()

	// Initialize all API routes (chat, theme, etc.)
	initRouters(srv, chatHandler, themeHandler)

	// Static file server for frontend pages
	initStaticFileServer(srv, cfg.Frontend.Dir, cfg.Frontend.CacheDisable)

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
