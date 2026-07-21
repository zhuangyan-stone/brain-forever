package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"BrainForever/infra/bktask"
	"BrainForever/infra/captcha"
	"BrainForever/infra/httpx"
	"BrainForever/infra/i18n"
	"BrainForever/infra/zylog"
	"BrainForever/internal/agent"
	"BrainForever/internal/config"
	"BrainForever/internal/logger"
	"BrainForever/internal/notify"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
	"BrainForever/internal/tasks"
	"BrainForever/internal/theme"
	"BrainForever/internal/user"
)

// ============================================================
// main
// ============================================================

func main() {
	// ============================================================
	// Command-line flags
	// ============================================================
	withNginx := flag.Bool("with-nginx", false, "Enable Nginx reverse proxy mode: disable built-in static file serving")
	flag.Parse()

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
	if envDSN := os.Getenv("PG_DSN"); envDSN != "" {
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
	// Initialize global PostgreSQL connection (thePGDBC)
	// Must be before InitTheUserStore, which depends on it.
	// ============================================================
	if cfg.Database.DSN == "" {
		theLogger.Fatalf("PG_DSN environment variable is required")
	}
	if err := store.InitPGDB(&cfg.Database); err != nil {
		theLogger.Fatalf("failed to initialize PostgreSQL: %v", err)
	}
	defer store.ClosePGDB()
	theLogger.Infof("PostgreSQL connection established")

	// ============================================================
	// Initialize all database schemas (single unified init.sql)
	// ============================================================
	const vectorDimension = 1024
	initSQL, err := store.InitSchema(vectorDimension)
	if err != nil {
		theLogger.Fatalf("failed to initialize database schema: %v", err)
	}
	if initSQL != "" {
		theLogger.Infof("executed schema: %s", initSQL)
	}
	theLogger.Infof("database schema initialized (dimension=%d)", vectorDimension)

	// ============================================================
	// Initialize global UserStore singleton (based on PostgreSQL)
	// Opens before HTTP server starts, closes after it stops
	// ============================================================
	if err := store.InitTheUserStore(theLogger); err != nil {
		theLogger.Fatalf("failed to initialize user store: %v", err)
	}
	defer store.CloseTheUserStore()
	theLogger.Infof("user store (PostgreSQL) initialized")

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
	// Initialize CaptchaProvider (click-based captcha)
	// ============================================================
	var captchaStore captcha.ICaptchaStore
	if sm := chatHandler.GetSessionManager(); sm.HasRedis() {
		captchaStore = cache.NewRedisCaptchaStore(sm.Redis().Client())
		theLogger.Infof("CaptchaStore: Redis backend")
	} else {
		captchaStore = cache.NewMemoryCaptchaStore()
		theLogger.Infof("CaptchaStore: in-memory backend (dev mode)")
	}

	captchaProvider, err := captcha.NewCaptchaProvider(
		context.Background(),
		cfg.Captcha.URLBase,
		cfg.Captcha.DirBase,
		captchaStore,
		theLogger,
	)
	if err != nil {
		theLogger.Fatalf("failed to initialize captcha provider: %v", err)
	}
	theLogger.Infof("captcha provider initialized (activeDir=%s, count=%d)", captchaProvider.ActiveDir(), captchaProvider.ActiveCount())

	// ============================================================
	// Initialize global background slow-task queue
	// ============================================================
	if cfg.TaskQueue.Enabled {
		tasks.InitTheBkTaskQueue(bktask.Config{
			CheckInterval: time.Duration(cfg.TaskQueue.CheckIntervalSeconds) * time.Second,
			WorkerCount:   cfg.TaskQueue.WorkerCount,
			QueueSize:     cfg.TaskQueue.QueueSize,
		}, theLogger)
		defer tasks.StopTheBkTaskQueue()
		theLogger.Infof("bkgnd task queue initialized (checkInterval=%ds, workers=%d, queueSize=%d)",
			cfg.TaskQueue.CheckIntervalSeconds, cfg.TaskQueue.WorkerCount, cfg.TaskQueue.QueueSize)

		// Register periodic trait extraction task.
		tasks.RegisterPeriodicTraitExtraction(
			cfg.TraitTask,
			agent.GetChatStore(),
			agent.GetBrainStore(),
			agent.GetLLMClients(),
			agent.GetEmbedderClients(),
			theLogger,
			defaultLang,
			cfg.TraitTask.DeduplicateEnabled,
			cfg.TraitTask.DeduplicateThreshold,
		)

		// Register periodic excerpt generation task.
		excerptStore := store.NewExcerptStore(theLogger)
		vdCache := cache.NewExcerptValueDictCache()
		dicts, err := excerptStore.ListAllValueDicts()
		if err != nil {
			theLogger.Fatalf("failed to load excerpt value dict. %v", err)
		}
		vdCache.Load(dicts)
		tasks.RegisterPeriodicExcerptGeneration(
			cfg.ExcerptTask,
			excerptStore,
			agent.GetLLMClients(),
			defaultLang,
			vdCache,
			theLogger,
		)

		// Register periodic session GC task.
		tasks.RegisterPeriodicSessionGC(
			cfg.SessionGCTask,
			chatHandler.GetSessionManager(),
			theLogger,
		)
	} else {
		theLogger.Infof("bkgnd task queue disabled by config")
	}

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

	// Initialize global API keys pool singleton (used by user package)
	config.InitApiKeysPool(cfg.ApiKeys)

	// Validate that all default providers have at least one API key configured.
	// If any default provider's key array is empty (or missing entirely),
	// the server cannot operate and must shut down immediately.
	if err := config.GetApiKeysPool().ValidateDefaultProviders(); err != nil {
		theLogger.Fatalf("API key configuration error: %v", err)
	}
	theLogger.Infof("API key pool validated — all default providers have keys")

	// Initialize theme handler for manifest and user handler for user operations
	themeHandler := theme.NewHandler()
	userHandler := user.NewHandler(
		chatHandler.GetSessionManager(),
		chatHandler.GetCookieName(),
		chatHandler.GetLogger(),
		chatHandler.GetAvatarDir(),
		chatHandler.GetSMSCodeCache(),
		captchaProvider,
	)

	// Initialize notify handler for internal notification endpoints (e.g., captcha refresh)
	notifyHandler := notify.NewHandler(captchaProvider)

	// Initialize all API routes (chat, theme, user, notify, etc.)
	initRouters(srv, chatHandler, themeHandler, userHandler, notifyHandler)

	// Static file server for frontend pages
	// Pass chatHandler for login check on index.html (302 redirect to /signin.html if anonymous)
	if *withNginx {
		// Nginx reverse proxy mode: only handle / and /index.html with login check,
		// other static files (/static/, /themes/, /signin/) are served by Nginx directly.
		initNginxModeStaticServer(srv, cfg.Frontend.Dir, chatHandler)
		theLogger.Infof("Nginx mode enabled: static files (except /) served by Nginx")
	} else {
		// Standard mode: Go serves both API and all static files.
		initStaticFileServer(srv, cfg.Frontend.Dir, cfg.Frontend.CacheDisable, chatHandler)
	}

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
