package config

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// ============================================================
// Global singleton — shared API keys pool
// ============================================================

var theApiKeysPool ApiKeysConfig

// InitApiKeysPool sets the global system-shared API keys pool.
// Must be called during startup after loading the config file.
// Fills in default provider names if not specified in config.
func InitApiKeysPool(cfg ApiKeysConfig) {
	if cfg.DefaultLLMProvider == "" {
		cfg.DefaultLLMProvider = "deepseek"
	}
	if cfg.DefaultWebSearchProvider == "" {
		cfg.DefaultWebSearchProvider = "zhipu"
	}
	if cfg.DefaultEmbeddingProvider == "" {
		cfg.DefaultEmbeddingProvider = "zhipu"
	}
	theApiKeysPool = cfg
}

// GetApiKeysPool returns the global system-shared API keys pool.
func GetApiKeysPool() ApiKeysConfig {
	return theApiKeysPool
}

// GetDefaultLLMProvider returns the default LLM provider name.
func GetDefaultLLMProvider() string {
	return theApiKeysPool.DefaultLLMProvider
}

// GetDefaultWebSearchProvider returns the default web search provider name.
func GetDefaultWebSearchProvider() string {
	return theApiKeysPool.DefaultWebSearchProvider
}

// GetDefaultEmbeddingProvider returns the default embedding provider name.
func GetDefaultEmbeddingProvider() string {
	return theApiKeysPool.DefaultEmbeddingProvider
}

// ============================================================
// Config -centralized configuration for the BrainForever server
//
// API keys for external services (LLM, Embedder, WebSearch) are
// now user-specific, stored per-user in the database. These global
// configs cover only infrastructure settings (server, DB, Redis, etc.).
// ============================================================

// Config is the top-level configuration for the agent layer.
type Config struct {
	Logger      LoggerConfig
	Server      ServerConfig
	Frontend    FrontendConfig
	Database    DatabaseConfig
	Redis       RedisConfig
	Captcha     CaptchaConfig
	SessionGC   SessionGCConfig   `toml:"session-gc"`
	ApiKeys     ApiKeysConfig     `toml:"api-keys"`
	TaskQueue   TaskQueueConfig   `toml:"bkgnd-task-queue"`
	TraitTask   TraitTaskConfig   `toml:"trait-task"`
	ExcerptTask ExcerptTaskConfig `toml:"excerpt-task"`
}

// DefaultConfig returns a Config populated with built-in default values.
// These defaults can be overridden by a TOML config file and/or environment variables.
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Name:              "brain-forever",
			Addr:              "[::]:8080",
			ReadTimeout:       30,
			ReadHeaderTimeout: 10,
			WriteTimeout:      0, // 0 = disabled -SSE streaming requires long-lived connections
			IdleTimeout:       60,
		},
		Frontend: FrontendConfig{
			Dir:          "./frontend",
			CacheDisable: false,
		},
		Logger: LoggerConfig{
			File:  "log/brain-forever.log",
			Level: "TRACE",
			Lang:  0,
		},
		Database: DatabaseConfig{
			DSN:          os.Getenv("MYSQL_DSN_d2brain"),
			MaxOpenConns: 25,
			MaxIdleConns: 5,
		},
		Redis: RedisConfig{
			Addr:     os.Getenv("REDIS_ADDR"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       0,
			PoolSize: 10,
		},
		Captcha: CaptchaConfig{
			URLBase: "/static/img/captchas/",
			DirBase: "./frontend/static/img/captchas/",
		},
		SessionGC: SessionGCConfig{
			AnonymousTTLMinutes: 60,   // 1 hour
			LoggedInTTLMinutes:  1440, // 24 hours
			IntervalMinutes:     10,   // 10 minutes
		},
		TaskQueue: TaskQueueConfig{
			Enabled:              true,
			CheckIntervalSeconds: 30,
			WorkerCount:          3,
			QueueSize:            100,
		},
		TraitTask: TraitTaskConfig{
			Enabled:              true,
			IntervalSeconds:      3600,
			ExtractDelayHours:    20,
			BatchLimit:           50,
			AllowedWindows:       [][]TimeOfDay{},
			DeduplicateEnabled:   false,
			DeduplicateThreshold: 0.95,
		},
		ExcerptTask: ExcerptTaskConfig{
			Enabled:           true,
			IntervalSeconds:   86400, // once per day
			ExtractDelayHours: 24,    // wait 24 hours after update before re-processing
			BatchLimit:        100,
			AllowedWindows:    [][]TimeOfDay{},
		},
	}
}

// LoadFromFile reads a TOML config file and overlays its values onto the Config.
// Only fields present in the TOML file are overwritten; all other fields retain
// their current values. Missing file (ENOENT) is silently ignored.
func (c *Config) LoadFromFile(path string) error {
	_, err := toml.DecodeFile(path, c)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file optional, silently ignore
		}
		return err
	}
	return nil
}

// ============================================================
// ApiKeysConfig — shared system API key pool
//
// Provides a global pool of API keys for external services (LLM,
// WebSearch, Embedding) configured in server.toml under [api-keys].
//
// Keys are identified by "purpose@provider", e.g.:
//   - llm@deepseek       — LLM chat via DeepSeek
//   - websearch@zhipu    — Web search via Zhipu
//   - embedding@ali      — Embedding via Alibaba DashScope
//
// The pool is populated once at startup (single-threaded TOML decode)
// and is read-only thereafter, so no locking is required.
// ============================================================

// ApiKeysConfig holds a read-only pool of system-shared API keys
// and default provider names.
type ApiKeysConfig struct {
	DefaultLLMProvider       string `toml:"default_llm_provider"`
	DefaultWebSearchProvider string `toml:"default_web_search_provider"`
	DefaultEmbeddingProvider string `toml:"default_embedding_provider"`
	keys                     map[string][]string
}

// UnmarshalTOML implements toml.Unmarshaler to decode a [api-keys] section
// directly into ApiKeysConfig. String values populate the default provider
// fields; array values become "purpose@provider" entries in the key pool.
//
// TOML example:
//
//	[api-keys]
//	default_llm_provider = "deepseek"
//	default_web_search_provider = "zhipu"
//	default_embedding_provider = "zhipu"
//	llm@deepseek = ["sk-abc123", "sk-def456"]
//	websearch@zhipu = ["key-789"]
func (a *ApiKeysConfig) UnmarshalTOML(i interface{}) error {
	m, ok := i.(map[string]interface{})
	if !ok {
		return nil
	}
	for k, v := range m {
		switch k {
		case "default_llm_provider", "default_web_search_provider", "default_embedding_provider":
			if s, ok := v.(string); ok {
				switch k {
				case "default_llm_provider":
					a.DefaultLLMProvider = s
				case "default_web_search_provider":
					a.DefaultWebSearchProvider = s
				case "default_embedding_provider":
					a.DefaultEmbeddingProvider = s
				}
			}
		default:
			list, ok := v.([]interface{})
			if !ok {
				continue
			}
			strs := make([]string, 0, len(list))
			for _, item := range list {
				if s, ok := item.(string); ok {
					strs = append(strs, s)
				}
			}
			if len(strs) > 0 {
				if a.keys == nil {
					a.keys = make(map[string][]string)
				}
				a.keys[k] = strs
			}
		}
	}
	return nil
}

// GetOne returns a random API key for the given purpose and provider.
// The lookup key is constructed as "purpose@provider".
// Returns an empty string if no keys are configured for this key.
func (a *ApiKeysConfig) GetOne(purpose, provider string) string {
	if a.keys == nil {
		return ""
	}

	key := purpose + "@" + provider
	vals := a.keys[key]

	if c := len(vals); c == 1 {
		return vals[0]
	} else if c == 0 {
		return ""
	} else {
		return vals[rand.Intn(len(vals))]
	}
}

// ValidateDefaultProviders checks that each default provider
// (LLM, WebSearch, Embedding) has at least one API key configured.
// If any default provider's key array is empty (or missing entirely),
// it returns an error listing which providers are missing keys.
func (a ApiKeysConfig) ValidateDefaultProviders() error {
	var missing []string

	if a.GetOne("llm", a.DefaultLLMProvider) == "" {
		missing = append(missing, "llm@"+a.DefaultLLMProvider)
	}
	if a.GetOne("websearch", a.DefaultWebSearchProvider) == "" {
		missing = append(missing, "websearch@"+a.DefaultWebSearchProvider)
	}
	if a.GetOne("embedding", a.DefaultEmbeddingProvider) == "" {
		missing = append(missing, "embedding@"+a.DefaultEmbeddingProvider)
	}

	if len(missing) > 0 {
		return fmt.Errorf("default API provider(s) have no keys configured. %v", missing)
	}
	return nil
}

// ServerConfig configures the HTTP server.
type ServerConfig struct {
	// Name is the server identifier, used in logs.
	Name string

	// Addr is the listen address, e.g. ":8080" or "[::]:8080".
	// Overridable by PROXY_ADDR environment variable.
	Addr string

	// ReadTimeout is the maximum duration for reading the entire request, including the body.
	ReadTimeout int // seconds, 0 = no timeout
	// ReadHeaderTimeout is the amount of time allowed to read request headers.
	ReadHeaderTimeout int // seconds, 0 = no timeout
	// WriteTimeout is the maximum duration before timing out writes of the response.
	WriteTimeout int // seconds, 0 = no timeout
	// IdleTimeout is the maximum amount of time to wait for the next request when keep-alives are enabled.
	IdleTimeout int // seconds, 0 = no timeout
}

// FrontendConfig configures the static file serving for the frontend.
type FrontendConfig struct {
	// Dir is the directory path for frontend static files.
	// Default: "./frontend".
	Dir string

	// CacheDisable disables browser caching for development.
	// When true, sets Cache-Control: no-cache headers on static files.
	CacheDisable bool
}

// LoggerConfig configures the golbal logger
type LoggerConfig struct {
	File             string
	Level            string   // TRACE, DEBUG, INFO, WARN, ERROR, FATAL
	Lang             int      // 0 en, 1 custom
	CustomLevelNames []string // Custom level names for LanguageCustom, e.g. {"TRACE","DEBUG","INFO","WARN","ERROR","FATAL","OFF"}
}

// ============================================================
// DatabaseConfig — PostgreSQL
// ============================================================

// DatabaseConfig configures the PostgreSQL database connection.
type DatabaseConfig struct {
	// DSN is the PostgreSQL data source name.
	// e.g. "postgres://user:password@127.0.0.1:5432/d2brain?sslmode=disable"
	DSN string

	// MaxOpenConns is the maximum number of open connections to the database.
	// Default: 25.
	MaxOpenConns int

	// MaxIdleConns is the maximum number of idle connections in the pool.
	// Default: 5.
	MaxIdleConns int
}

// ============================================================
// RedisConfig — Redis
// ============================================================

// RedisConfig configures the Redis connection.
type RedisConfig struct {
	// Addr is the Redis server address, e.g. "127.0.0.1:6379".
	Addr string

	// Password is the Redis password (empty if no auth).
	Password string

	// DB is the Redis database number to use.
	// Default: 0.
	DB int

	// PoolSize is the maximum number of socket connections.
	// Default: 10.
	PoolSize int
}

// ============================================================
// CaptchaConfig configures the captcha recognition module.
// ============================================================

// CaptchaConfig configures the captcha recognition settings.
type CaptchaConfig struct {
	// URLBase is the URL base path for captcha images, e.g., "/captcha/".
	URLBase string
	// DirBase is the server local file system path for captcha images and data,
	// e.g., "./data/captchas/". It contains d1/ and d2/ subdirectories,
	// each with png/ and json/ subdirectories.
	DirBase string
}

// ============================================================
// SessionGCConfig — In-memory session garbage collector
// ============================================================

// SessionGCConfig configures the in-memory session garbage collector.
// These values are read from the TOML config file under [session-gc].
// If not configured, DefaultConfig() provides sensible defaults.
type SessionGCConfig struct {
	// AnonymousTTLMinutes is the max idle time (minutes) before an
	// anonymous (not logged in) session is evicted from memory.
	// Default: 60 (1 hour).
	AnonymousTTLMinutes int `toml:"anonymous_ttl_minutes"`

	// LoggedInTTLMinutes is the max idle time (minutes) before a
	// logged-in session is evicted from memory.
	// Default: 1440 (24 hours).
	LoggedInTTLMinutes int `toml:"logged_in_ttl_minutes"`

	// IntervalMinutes is how often (minutes) the GC sweep runs.
	// Default: 10.
	IntervalMinutes int `toml:"interval_minutes"`
}

// ============================================================
// TaskQueueConfig — Global background slow-task queue
// ============================================================

// TaskQueueConfig configures the global background slow-task queue.
// These values are read from the TOML config file under [bkgnd-task-queue].
// If not configured, DefaultConfig() provides sensible defaults.
type TaskQueueConfig struct {
	// Enabled enables the background task queue. Default: true.
	Enabled bool `toml:"enabled"`

	// CheckIntervalSeconds is how often (seconds) the queue checks for due tasks.
	// Default: 30.
	CheckIntervalSeconds int `toml:"check_interval_seconds"`

	// WorkerCount is the maximum number of concurrent task executions.
	// Default: 3. 0 or negative = unlimited.
	WorkerCount int `toml:"worker_count"`

	// QueueSize is the maximum number of queued tasks.
	// Default: 100.
	QueueSize int `toml:"queue_size"`
}

// TimeOfDay represents a time within a day (hour:minute only, no date).
// Internally stores minutes since midnight (0-1439) for direct comparison.
// Implements encoding.TextUnmarshaler so BurntSushi/toml decodes "HH:MM"
// strings directly into this type.
type TimeOfDay int

// Minutes returns the minutes since midnight.
func (t TimeOfDay) Minutes() int { return int(t) }

// UnmarshalText implements encoding.TextUnmarshaler for "HH:MM" format.
func (t *TimeOfDay) UnmarshalText(text []byte) error {
	s := string(text)
	if len(s) < 5 || s[2] != ':' {
		return fmt.Errorf("invalid time format %q, expected HH:MM", s)
	}
	hour := int(s[0]-'0')*10 + int(s[1]-'0')
	minute := int(s[3]-'0')*10 + int(s[4]-'0')
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return fmt.Errorf("invalid time %q, out of range", s)
	}
	*t = TimeOfDay(hour*60 + minute)
	return nil
}

// ============================================================
// TraitTaskConfig — Periodic personal trait extraction
// ============================================================

// TraitTaskConfig configures the periodic background task that scans
// chat_sessions for chats pending personal trait extraction and processes them.
// These values are read from the TOML config file under [trait-extraction-task].
// If not configured, DefaultConfig() provides sensible defaults.
type TraitTaskConfig struct {
	// Enabled enables the periodic trait extraction task. Default: true.
	Enabled bool `toml:"enabled"`

	// IntervalSeconds is how often (seconds) the task runs its scan-and-extract cycle.
	// Default: 3600 (1 hour).
	IntervalSeconds int `toml:"interval_seconds"`

	// ExtractDelayHours is the threshold (hours) for re-extraction:
	// chats with extracted_at older than update_at minus this many hours are eligible.
	// Default: 20.
	ExtractDelayHours int `toml:"extract_delay_hours"`

	// BatchLimit is the maximum number of chat sessions processed per cycle.
	// Default: 50.
	BatchLimit int `toml:"batch_limit"`

	// AllowedWindows restricts execution to specific time-of-day windows.
	// Each entry is a [start, stop] pair of TimeOfDay values in "HH:MM" format,
	// defining a half-open [start, stop) range.
	// Empty slice (default) means always allowed (00:00-24:00).
	// If stop <= start, the window wraps past midnight (e.g. ["22:00", "06:00"]).
	// TOML example: allowed_windows = [["02:00", "06:00"], ["22:00", "23:59"]]
	AllowedWindows [][]TimeOfDay `toml:"allowed_windows"`

	// DeduplicateEnabled enables trait embedding deduplication before insertion.
	// When enabled, each new trait's embedding is compared against existing traits
	// in the same chat; if the cosine similarity exceeds DeduplicateThreshold,
	// the trait is skipped to avoid storing near-duplicate entries.
	// Default: false (deduplication disabled).
	DeduplicateEnabled bool `toml:"deduplicate_enabled"`

	// DeduplicateThreshold is the cosine similarity threshold (0.0-1.0) for
	// trait deduplication. Only meaningful when DeduplicateEnabled is true.
	// A new trait with similarity >= this value to any existing trait in the
	// same chat is considered a duplicate and will not be stored.
	// Default: 0.95 (recommended).
	DeduplicateThreshold float64 `toml:"deduplicate_threshold"`
}

// ============================================================
// ExcerptTaskConfig — Periodic excerpt generation
// ============================================================

// ExcerptTaskConfig configures the periodic background task that scans chat
// messages and generates user quote excerpts via LLM.
type ExcerptTaskConfig struct {
	// Enabled enables the periodic excerpt generation task. Default: true.
	Enabled bool `toml:"enabled"`

	// IntervalSeconds is how often (seconds) the task runs its scan-and-generate cycle.
	// Default: 86400 (once per day).
	IntervalSeconds int `toml:"interval_seconds"`

	// ExtractDelayHours is the threshold (hours) for re-extraction:
	// chats with processed_at older than update_at minus this many hours are eligible
	// for re-processing. This prevents re-processing chats that were just updated.
	// Default: 24.
	ExtractDelayHours int `toml:"extract_delay_hours"`

	// BatchLimit is the maximum number of chat sessions processed per cycle.
	// Default: 100.
	BatchLimit int `toml:"batch_limit"`

	// AllowedWindows restricts execution to specific time-of-day windows.
	// Same semantics as TraitExtractionTaskConfig.AllowedWindows.
	// Empty slice (default) means always allowed.
	AllowedWindows [][]TimeOfDay `toml:"allowed_windows"`
}

// isAllowedTimePoint is the shared implementation of time-window checking.
// windows defines the allowed time-of-day ranges (half-open [start, stop)).
// An empty windows slice means all times are allowed.
// Windows that wrap past midnight (stop <= start) are handled correctly.
func isAllowedTimePoint(windows [][]TimeOfDay, t time.Time) bool {
	if len(windows) == 0 {
		return true
	}

	currentMinutes := t.Hour()*60 + t.Minute()

	for _, w := range windows {
		if len(w) != 2 {
			continue
		}
		startMinutes := w[0].Minutes()
		stopMinutes := w[1].Minutes()

		if startMinutes <= stopMinutes {
			// Normal window: [start, stop)
			if currentMinutes >= startMinutes && currentMinutes < stopMinutes {
				return true
			}
		} else {
			// Wrapping window: [start, 24:00) ∪ [00:00, stop)
			if currentMinutes >= startMinutes || currentMinutes < stopMinutes {
				return true
			}
		}
	}
	return false
}

// IsAllowedTimePoint returns true if the given time falls within at least one
// allowed window. An empty AllowedWindows slice means all times are allowed.
// Windows that wrap past midnight (stop <= start) are handled correctly.
func (c *ExcerptTaskConfig) IsAllowedTimePoint(t time.Time) bool {
	return isAllowedTimePoint(c.AllowedWindows, t)
}

// IsAllowedTimePoint returns true if the given time falls within at least one
// allowed window. An empty AllowedWindows slice means all times are allowed.
// Windows that wrap past midnight (stop <= start) are handled correctly.
func (c *TraitTaskConfig) IsAllowedTimePoint(t time.Time) bool {
	return isAllowedTimePoint(c.AllowedWindows, t)
}
