package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"BrainForever/internal/store"
	"BrainForever/internal/store/dbc"
)

// ============================================================
// SessionManager
// ============================================================

// SessionManager manages all user sessions.
// It does not hold any database connections; databases are accessed
// via the global dbc package or theUserStore singleton.
// Login state is persisted in Redis for restart resilience and
// potential horizontal scaling.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*session
	redis    *store.RedisSessionStore // Redis-backed login state (nil = Redis unavailable)
	ctx      context.Context          // Background context for Redis operations
}

// SetRedisStore attaches a Redis session store to the SessionManager.
// Must be called before any session operations if Redis is available.
// If redisStore is nil, session management operates in pure in-memory mode
// (no persistence across restarts, same as before).
func (sm *SessionManager) SetRedisStore(redisStore *store.RedisSessionStore) {
	sm.redis = redisStore
}

// Close releases all sessions. No database stores to close since
// they are opened on-demand and closed after use.
func (sm *SessionManager) Close() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Clear all sessions. No long-lived DB stores to close.
	sm.sessions = make(map[string]*session)
}

// NewSessionManager creates a SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*session),
		ctx:      context.Background(),
	}
}

// GetOrCreate gets or creates a session for the given sessionID.
// If Redis is available and the session doesn't exist in memory,
// it attempts to restore the login state from Redis.
// No database stores are opened at creation time.
func (sm *SessionManager) GetOrCreate(sessionID string) *session {
	sm.mu.RLock()
	s, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if ok {
		s.mu.Lock()
		s.lastActivity = time.Now()
		s.mu.Unlock()
		return s
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check
	if s, ok := sm.sessions[sessionID]; ok {
		s.mu.Lock()
		s.lastActivity = time.Now()
		s.mu.Unlock()
		return s
	}

	// Try to restore login state from Redis
	var restoredID int64
	var restoredSN string
	var restoredSettings store.UserSettings
	if sm.redis != nil {
		if id, sn, settingsJSON, found, err := sm.redis.GetLoginSession(sm.ctx, sessionID); err == nil && found {
			restoredID = id
			restoredSN = sn
			if settingsJSON != "" {
				restoredSettings.FromString(settingsJSON)
			}
		}
	}

	s = &session{
		id:           sessionID,
		lastActivity: time.Now(),
		user: sessionUser{
			ID:          restoredID,
			SN:          restoredSN,
			settings:    restoredSettings,
			currentChat: &chat{},
		},
	}

	sm.sessions[sessionID] = s
	return s
}

// Remove removes the session for the given sessionID (optional)
func (sm *SessionManager) Remove(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, sessionID)
}

// DeleteMessage deletes a user message and all associated messages (AI reply, etc.)
// that share the same source ID. It finds the first message with the given msgID,
// then removes all consecutive messages with the same ID. Stops at the first message
// with a different ID. Returns an error if the msgID is not found.
func (sm *SessionManager) DeleteMessage(sessionID string, msgID int64) error {
	sm.mu.RLock()
	s, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session not found")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = time.Now()

	if s.user.currentChat.dbChat == nil {
		return fmt.Errorf("no DB session")
	}
	chatID := s.user.currentChat.dbChat.ID
	if chatID == 0 {
		return fmt.Errorf("no DB session")
	}

	// Open chat store on-demand via dbc, delete, close
	chatStore, err := dbc.OpenLocalChatDB(s.user.ID, s.user.SN)
	if err != nil {
		return fmt.Errorf("failed to open chat store: %w", err)
	}
	defer dbc.CloseLocalChatDB(chatStore)

	// Delete messages and their web sources from DB
	return chatStore.DeleteMessageGroup(chatID, int(msgID))
}
