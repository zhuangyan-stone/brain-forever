package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
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
	"BrainForever/internal/user"
)

// ============================================================
// main
// ============================================================

func main() {
	// ============================================================
	// Build configuration (defaults → TOML file → env var overrides)
	// ============================================================

	// Step 1: Start with built-in defaults
	cfg := config.DefaultConfig()

	// Step 2: Overlay values from optional TOML config file
	// Fields present in the TOML file override defaults; missing sections are left as-is.
	if err := cfg.LoadFromFile("bin/settings/server.toml"); err != nil {
		log.Fatalf("failed to load config file bin/settings/server.toml: %v", err)
	}

	// Step 3: Environment variable overrides (highest priority)
	//
	// Database DSN — always from env var for security (password in file is discouraged)
	if envDSN := os.Getenv("MYSQL_DSN_d2brain"); envDSN != "" {
		cfg.Database.DSN = envDSN
	}
	// Redis — from env vars if set (empty = no Redis)
	if envAddr := os.Getenv("REDIS_ADDR"); envAddr != "" {
		cfg.Redis.Addr = envAddr
	}
	if envPassword := os.Getenv("REDIS_PASSWORD"); envPassword != "" {
		cfg.Redis.Password = envPassword
	}
	// Server address — overridable via PROXY_ADDR
	if envAddr := os.Getenv("PROXY_ADDR"); envAddr != "" {
		cfg.Server.Addr = envAddr
	}
	// Development cache disable
	if envDisable := os.Getenv("DEV_CACHE_DISABLE"); envDisable == "true" {
		cfg.Frontend.CacheDisable = true
	}

	// Resolve frontend directory to an absolute path.
	// Relative paths are resolved against the current working directory
	// (set via VS Code launch.json cwd or by running from the project root).
	if resolved, err := filepath.Abs(cfg.Frontend.Dir); err != nil {
		log.Fatalf("failed to resolve frontend dir %q: %v", cfg.Frontend.Dir, err)
	} else {
		cfg.Frontend.Dir = resolved
	}

	if err := logger.CreateTheLogger(zylog.NameToLevel(cfg.Logger.Level), cfg.Logger.File,
		zylog.Language(cfg.Logger.Lang), cfg.Logger.CustomLevelNames); err != nil {
		log.Fatalf("create the logger fail. %v", err)
	}

	theLogger := logger.TheLogger()

	// first log
	theLogger.Infof("the logger is created with level. level %s. file %s", cfg.Logger.Level, cfg.Logger.File)

	// ============================================================
	// Ensure data directory exists for SQLite databases
	// ============================================================
	if err := os.MkdirAll(cfg.Data.Dir, 0755); err != nil {
		theLogger.Fatalf("failed to create data directory %q: %v", cfg.Data.Dir, err)
	}

	// ============================================================
	// Initialize global MySQL connection (theMySQLDBC)
	// Must be before InitTheUserStore, which depends on it.
	// ============================================================
	if cfg.Database.DSN == "" {
		theLogger.Fatalf("MYSQL_DSN_d2brain environment variable is required")
	}
	if err := store.InitMySQLDB(cfg.Database.DSN); err != nil {
		theLogger.Fatalf("failed to initialize MySQL: %v", err)
	}
	defer store.CloseMySQLDB()
	theLogger.Infof("MySQL connection established")

	// ============================================================
	// Initialize global UserStore singleton (based on MySQL)
	// Opens before HTTP server starts, closes after it stops
	// ============================================================
	if err := store.InitTheUserStore(cfg.Data.Dir); err != nil {
		theLogger.Fatalf("failed to initialize user store: %v", err)
	}
	defer store.CloseTheUserStore()
	theLogger.Infof("user store (MySQL) initialized")

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
	// Initialize agent (Embedder, VectorStore, LLMClient, WebSearchClient, Redis)
	// ============================================================
	chatHandler, err := agent.InitAgent(ctx, cfg, "brain_go_session", defaultLang, theLogger)
	if err != nil {
		theLogger.Fatalf("failed to initialize agent: %v", err)
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

	// Initialize theme handler for manifest and user handler for user operations
	themeHandler := theme.NewHandler()
	userHandler := user.NewHandler(
		chatHandler.GetSessionManager(),
		chatHandler.GetCookieName(),
		chatHandler.GetLogger(),
		chatHandler.GetAvatarDir(),
		chatHandler.GetSMSCodeCache(),
	)

	// Initialize all API routes (chat, theme, user, etc.)
	initRouters(srv, chatHandler, themeHandler, userHandler)

	// Static file server for frontend pages
	// Pass chatHandler for login check on index.html (302 redirect to /signin.html if anonymous)
	initStaticFileServer(srv, cfg.Frontend.Dir, cfg.Frontend.CacheDisable, chatHandler)

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
