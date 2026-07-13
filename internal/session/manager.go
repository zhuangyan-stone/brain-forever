package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"BrainForever/internal/agent/llmtypes"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
)

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

// Close releases all sessions. No database stores to close since
// they are opened on-demand and closed after use.
func (m *Manager) Close() {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	// Clear all sessions. No long-lived DB stores to close.
	m.Sessions = make(map[string]*Session)
}

// NewManager creates a Manager.
func NewManager() *Manager {
	return &Manager{
		Sessions: make(map[string]*Session),
		Ctx:      context.Background(),
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

	// Use global ChatStore (PostgreSQL connection pool)
	chatStore := store.NewChatStore()
	return chatStore.DeleteMessageGroup(chatID, int(msgID))
}
