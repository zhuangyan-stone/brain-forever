package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"BrainForever/infra/zylog"
	"BrainForever/internal/agent/llmtypes"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
)

// ============================================================
// GCConfig — In-memory session garbage collector configuration
// ============================================================

// GCConfig holds the runtime configuration for the in-memory session GC.
type GCConfig struct {
	// AnonymousTTL is the max idle time before an anonymous session is evicted.
	AnonymousTTL time.Duration
	// LoggedInTTL is the max idle time before a logged-in session is evicted.
	LoggedInTTL time.Duration
	// Interval is how often the GC sweep runs.
	Interval time.Duration
}

// DefaultGCConfig returns a GCConfig with sensible built-in defaults.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		AnonymousTTL: 1 * time.Hour,
		LoggedInTTL:  24 * time.Hour,
		Interval:     10 * time.Minute,
	}
}

// FromTOMLConfig converts a TOML-derived session GC config (in minutes)
// to a runtime GCConfig (with time.Duration).
// Accepts a SessionGCConfigTOML struct which mirrors config.SessionGCConfig
// to avoid a direct import dependency on the config package.
func FromTOMLConfig(cfg SessionGCConfigTOML) GCConfig {
	return GCConfig{
		AnonymousTTL: time.Duration(cfg.AnonymousTTLMinutes) * time.Minute,
		LoggedInTTL:  time.Duration(cfg.LoggedInTTLMinutes) * time.Minute,
		Interval:     time.Duration(cfg.IntervalMinutes) * time.Minute,
	}
}

// SessionGCConfigTOML mirrors config.SessionGCConfig to avoid a direct
// import dependency from the session package to the config package.
type SessionGCConfigTOML struct {
	AnonymousTTLMinutes int
	LoggedInTTLMinutes  int
	IntervalMinutes     int
}

// ============================================================
// Manager
// ============================================================

// Manager manages all user sessions.
// It does not hold any database connections; databases are accessed
// via the global dbc package or theUserStore singleton.
// Login state is persisted in Redis for restart resilience and
// potential horizontal scaling.
type Manager struct {
	Mu       sync.RWMutex
	Sessions map[string]*Session
	redis    *cache.RedisSessionStore // unexported: use Redis() or HasRedis()
	Ctx      context.Context          // Background context for Redis operations
	logger   zylog.Logger
	gcConfig GCConfig // in-memory session GC config
}

// SetRedisStore attaches a Redis session store to the Manager.
// Must be called before any session operations.
func (m *Manager) SetRedisStore(redisStore *cache.RedisSessionStore) {
	m.redis = redisStore
}

// Redis returns the Redis session store, panicking if it's nil.
// Acts like C++ assert: business code assumes Redis is always configured.
func (m *Manager) Redis() *cache.RedisSessionStore {
	if m.redis == nil {
		panic("session: Redis store is nil")
	}
	return m.redis
}

// HasRedis returns true if a Redis session store is configured.
// Used by captcha code that supports in-memory fallback.
func (m *Manager) HasRedis() bool {
	return m.redis != nil
}

// Close releases all sessions.
// No database stores to close since they are opened on-demand and closed after use.
func (m *Manager) Close() {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	// Clear all sessions. No long-lived DB stores to close.
	m.Sessions = make(map[string]*Session)
}

// NewManager creates a Manager with the given GC configuration.
// Pass DefaultGCConfig() if no custom config is needed.
func NewManager(logger zylog.Logger, gcConfig GCConfig) *Manager {
	return &Manager{
		Sessions: make(map[string]*Session),
		Ctx:      context.Background(),
		logger:   logger,
		gcConfig: gcConfig,
	}
}

// GetOrCreate gets or creates a session for the given sessionID.
// If Redis is available and the session doesn't exist in memory,
// it attempts to restore the login state from Redis.
// No database stores are opened at creation time.
func (m *Manager) GetOrCreate(sessionID string) *Session {
	m.Mu.RLock()
	s, ok := m.Sessions[sessionID]
	m.Mu.RUnlock()

	if ok {
		s.Mu.Lock()
		s.LastActivity = time.Now()
		isLoggedIn := !s.IsAnonymous()
		s.Mu.Unlock()

		// Refresh Redis TTL for active logged-in sessions
		if isLoggedIn {
			m.Redis().RefreshTTL(m.Ctx, sessionID)
		}
		return s
	}

	m.Mu.Lock()
	defer m.Mu.Unlock()

	// Double-check
	if s, ok := m.Sessions[sessionID]; ok {
		s.Mu.Lock()
		s.LastActivity = time.Now()
		isLoggedIn := !s.IsAnonymous()
		s.Mu.Unlock()

		// Refresh Redis TTL for active logged-in sessions
		if isLoggedIn {
			m.Redis().RefreshTTL(m.Ctx, sessionID)
		}
		return s
	}

	// Try to restore login state from Redis
	var restoredSettings store.UserSettings
	if loginData, err := m.Redis().GetLoginSession(m.Ctx, sessionID); err == nil && loginData != nil {
		if loginData.Settings != "" {
			restoredSettings.FromString(loginData.Settings)
		}

		s = &Session{
			ID:           sessionID,
			LastActivity: time.Now(),
			User: SessionUser{
				ID:          loginData.UserID,
				SN:          loginData.UserSN,
				No:          loginData.No,
				Nickname:    loginData.Nickname,
				Settings:    restoredSettings,
				CurrentChat: &llmtypes.Chat{},
			},
		}
	} else {
		s = &Session{
			ID:           sessionID,
			LastActivity: time.Now(),
			User: SessionUser{
				CurrentChat: &llmtypes.Chat{},
			},
		}
	}

	m.Sessions[sessionID] = s
	return s
}

// Remove removes the session for the given sessionID (optional)
func (m *Manager) Remove(sessionID string) {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	delete(m.Sessions, sessionID)
}

// ============================================================
// GC — In-memory session garbage collector
// ============================================================

// GCOnce performs one sweep of expired sessions.
// Exported for use as a periodic task via the background task queue.
func (m *Manager) GCOnce() {
	m.gc()
}

// gc performs one sweep of expired sessions.
func (m *Manager) gc() {
	// Snapshot the current state under read lock.
	type sessionInfo struct {
		id           string
		lastActivity time.Time
		isAnonymous  bool
	}

	m.Mu.RLock()
	infos := make([]sessionInfo, 0, len(m.Sessions))
	var anonymousCount, loggedInCount int
	for id, s := range m.Sessions {
		s.Mu.Lock()
		isAnon := s.IsAnonymous()
		infos = append(infos, sessionInfo{
			id:           id,
			lastActivity: s.LastActivity,
			isAnonymous:  isAnon,
		})
		s.Mu.Unlock()
		if isAnon {
			anonymousCount++
		} else {
			loggedInCount++
		}
	}
	m.Mu.RUnlock()

	// Determine which sessions are expired.
	var expired []string
	var expiredAnon, expiredLoggedIn int
	for _, info := range infos {
		ttl := m.gcConfig.LoggedInTTL
		if info.isAnonymous {
			ttl = m.gcConfig.AnonymousTTL
		}
		if time.Since(info.lastActivity) > ttl {
			expired = append(expired, info.id)
			if info.isAnonymous {
				expiredAnon++
			} else {
				expiredLoggedIn++
			}
		}
	}

	// Remove expired sessions under write lock.
	var remainingAnon, remainingLoggedIn int
	if len(expired) > 0 {
		m.Mu.Lock()
		for _, id := range expired {
			// delete is safe even if the key no longer exists (GC concurrent safety).
			delete(m.Sessions, id)
		}
		m.Mu.Unlock()

		remainingAnon = anonymousCount - expiredAnon
		remainingLoggedIn = loggedInCount - expiredLoggedIn

		m.logger.Infof("session GC cleaned up %d expired (anonymous: %d/%d, logged-in: %d/%d), %d remaining",
			len(expired),
			expiredAnon, anonymousCount,
			expiredLoggedIn, loggedInCount,
			len(m.Sessions))
	} else {
		remainingAnon = anonymousCount
		remainingLoggedIn = loggedInCount

		m.logger.Infof("session GC sweep: 0 expired, %d total (%d anonymous, %d logged-in)",
			len(infos), anonymousCount, loggedInCount)
	}

	// Write GC stats to Redis (best-effort, non-blocking).
	if m.HasRedis() {
		m.Redis().SetGCStats(m.Ctx, &cache.GCStats{
			ExpiredAnonymous: expiredAnon,
			ExpiredLoggedIn:  expiredLoggedIn,
			LoggedInUsers:    remainingLoggedIn,
			AnonymousUsers:   remainingAnon,
		})
	}
}

// DeleteMessage deletes a user message and all associated messages (AI reply, etc.)
// that share the same source ID. It finds the first message with the given msgID,
// then removes all consecutive messages with the same ID. Stops at the first message
// with a different ID. Returns an error if the msgID is not found.
func (m *Manager) DeleteMessage(sessionID string, msgID int64) error {
	m.Mu.RLock()
	s, ok := m.Sessions[sessionID]
	m.Mu.RUnlock()
	if !ok {
		return fmt.Errorf("session not found")
	}

	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.LastActivity = time.Now()

	if s.User.CurrentChat.DBCHat == nil {
		return fmt.Errorf("no DB session")
	}
	chatID := s.User.CurrentChat.DBCHat.ID
	if chatID == 0 {
		return fmt.Errorf("no DB session")
	}

	// Delete from DB first
	chatStore := store.NewChatStore(m.logger)
	if err := chatStore.DeleteMessageGroup(chatID, int(msgID)); err != nil {
		return err
	}

	// Remove deleted messages from the in-memory cache.
	kept := make([]llmtypes.Message, 0, len(s.User.CurrentChat.Messages))
	for _, m := range s.User.CurrentChat.Messages {
		if m.ID != msgID {
			kept = append(kept, m)
		}
	}
	s.User.CurrentChat.Messages = kept

	return nil
}
