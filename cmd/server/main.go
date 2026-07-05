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
	"strings"
	"sync"
	"syscall"
	"time"

	"BrainForever/infra/httpx"
	"BrainForever/infra/i18n"
	"BrainForever/infra/zylog"
	local "BrainForever/internal"
	"BrainForever/internal/agent"
	"BrainForever/internal/config"
	"BrainForever/internal/logger"
	"BrainForever/internal/store"

	"github.com/BurntSushi/toml"
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

	// Initialize all API routes
	local.InitRouters(srv, chatHandler)

	// ============================================================
	// Theme management API
	//   - theme manifest (themes[] list) comes from frontend/themes/manifest.json (source code)
	//   - user's theme selection (actived/actived-light/actived-dark) stored in local-server.toml (runtime config)
	// ============================================================

	// themeConfigFile is the path to the runtime theme configuration.
	const themeConfigFile = "./local-server.toml"

	// themeRuntime holds the runtime theme config read from / written to local-server.toml.
	type themeRuntime struct {
		Theme struct {
			Actived      string `toml:"actived"`
			ActivedLight string `toml:"actived-light"`
			ActivedDark  string `toml:"actived-dark"`
		} `toml:"theme"`
	}

	// readThemeConfig reads the theme runtime config from local-server.toml.
	// If the file doesn't exist or is malformed, returns sensible defaults.
	readThemeConfig := func() themeRuntime {
		var rt themeRuntime
		if _, err := toml.DecodeFile(themeConfigFile, &rt); err != nil {
			rt.Theme.Actived = "light"
			rt.Theme.ActivedLight = ""
			rt.Theme.ActivedDark = ""
		}
		return rt
	}

	// writeThemeConfig writes the theme runtime config to local-server.toml.
	// Uses a sync.Mutex to prevent concurrent writes.
	var (
		themeMu     sync.Mutex
		themeMuInit sync.Once
	)
	writeThemeConfig := func(rt themeRuntime) error {
		themeMuInit.Do(func() { /* lazy init, mutex ready */ })
		themeMu.Lock()
		defer themeMu.Unlock()

		// Read existing file to preserve other sections (e.g. server config)
		type fullConfig struct {
			Theme struct {
				Actived      string `toml:"actived"`
				ActivedLight string `toml:"actived-light"`
				ActivedDark  string `toml:"actived-dark"`
			} `toml:"theme"`
		}
		var cfg fullConfig
		if _, err := toml.DecodeFile(themeConfigFile, &cfg); err != nil {
			// File doesn't exist or is empty; start fresh
			cfg = fullConfig{}
		}
		cfg.Theme = rt.Theme

		var buf strings.Builder
		if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
			return err
		}
		return os.WriteFile(themeConfigFile, []byte(buf.String()), 0644)
	}

	// Build the merged response for GET /api/themes:
	//   themes[] from manifest.json (source code)
	//   actived/actived-light/actived-dark from local-server.toml (runtime config)
	srv.GET("/api/themes", func(w http.ResponseWriter, r *http.Request) {
		// Read themes list from manifest.json (source code)
		manifestRaw, err := os.ReadFile("./frontend/themes/manifest.json")
		if err != nil {
			http.Error(w, `{"error":"cannot read theme manifest"}`, http.StatusInternalServerError)
			return
		}
		var manifest struct {
			Themes []any `json:"themes"`
		}
		if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
			http.Error(w, `{"error":"invalid manifest JSON"}`, http.StatusInternalServerError)
			return
		}

		// Read runtime config from local-server.toml
		rt := readThemeConfig()

		// Merge into response
		resp := map[string]any{
			"themes":        manifest.Themes,
			"actived":       rt.Theme.Actived,
			"actived-light": rt.Theme.ActivedLight,
			"actived-dark":  rt.Theme.ActivedDark,
			"description":   "第2大脑 外源主题清单",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	srv.POST("/api/themes", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Actived      string `json:"actived"`
			ActivedLight string `json:"actived-light"`
			ActivedDark  string `json:"actived-dark"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}

		// Only write to local-server.toml — never touch manifest.json
		rt := themeRuntime{}
		rt.Theme.Actived = req.Actived
		rt.Theme.ActivedLight = req.ActivedLight
		rt.Theme.ActivedDark = req.ActivedDark

		if err := writeThemeConfig(rt); err != nil {
			http.Error(w, `{"error":"write error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

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
