// Package cache provides Redis-backed caching for temporary data
// such as session state, SMS verification codes, etc.
package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ============================================================
// RedisSessionStore
//
// Stores login session state (user_id, user_sn) in Redis.
// The session cookie ID is used as the Redis key so that:
//   - Server restart preserves login state (sessions survive reboot)
//   - Multiple server instances can share the same Redis (horizontal scaling)
//
// Only login identity is stored in Redis; business data (chats, currentChat)
// remains in the in-memory session struct and per-user SQLite files.
// ============================================================

const (
	// sessionKeyPrefix is the Redis key prefix for session data.
	sessionKeyPrefix = "session:"

	// sessionTTL is the time-to-live for session entries in Redis.
	// Must match the cookie MaxAge (7 days).
	sessionTTL = 7 * 24 * time.Hour
)

// LoginSessionData holds the user login session data persisted in Redis.
// It is a subset of session.SessionUser — kept here as a standalone struct
// to avoid circular imports between the cache and session packages.
type LoginSessionData struct {
	UserID   int64
	UserSN   string
	No       string
	Nickname string
	Settings string // JSON-serialized UserSettings
}

// RedisSessionStore wraps a Redis client for session operations.
type RedisSessionStore struct {
	client *redis.Client
}

// NewRedisSessionStore creates a new RedisSessionStore.
func NewRedisSessionStore(addr, password string, db int) *RedisSessionStore {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisSessionStore{client: rdb}
}

// Client returns the underlying Redis client.
// Used by other cache components (e.g., SMSCodeCache) to share the same connection pool.
func (rs *RedisSessionStore) Client() *redis.Client {
	return rs.client
}

// Close closes the underlying Redis connection.
func (rs *RedisSessionStore) Close() error {
	if rs.client != nil {
		return rs.client.Close()
	}
	return nil
}

// Ping checks if Redis is reachable.
func (rs *RedisSessionStore) Ping(ctx context.Context) error {
	return rs.client.Ping(ctx).Err()
}

// sessionKey returns the Redis key for a given session ID.
func sessionKey(sessionID string) string {
	return sessionKeyPrefix + sessionID
}

// ============================================================
// Session CRUD
// ============================================================

// SetLoginSession stores the user's login session in Redis.
// settingsJSON is the JSON-serialized UserSettings (API keys, theme, etc.).
func (rs *RedisSessionStore) SetLoginSession(ctx context.Context, sessionID string, data *LoginSessionData) error {
	key := sessionKey(sessionID)
	now := time.Now().UTC().Format(time.RFC3339)

	err := rs.client.HSet(ctx, key,
		"user_id", strconv.FormatInt(data.UserID, 10),
		"user_sn", data.UserSN,
		"no", data.No,
		"nickname", data.Nickname,
		"settings", data.Settings,
		"created_at", now,
		"last_active", now,
	).Err()
	if err != nil {
		return fmt.Errorf("redis: failed to set login session. %w", err)
	}

	// Set TTL so expired sessions are automatically cleaned up
	rs.client.Expire(ctx, key, sessionTTL)
	return nil
}

// GetLoginSession retrieves login state from Redis.
// Returns nil if the session does not exist or is malformed.
func (rs *RedisSessionStore) GetLoginSession(ctx context.Context, sessionID string) (*LoginSessionData, error) {
	key := sessionKey(sessionID)

	data, err := rs.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis: failed to get login session. %w", err)
	}

	if len(data) == 0 {
		return nil, nil // session not found
	}

	uidStr, hasUID := data["user_id"]
	snStr, hasSN := data["user_sn"]
	if !hasUID || !hasSN {
		// Malformed entry — clean it up
		rs.client.Del(ctx, key)
		return nil, nil
	}

	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		rs.client.Del(ctx, key)
		return nil, nil
	}

	return &LoginSessionData{
		UserID:   uid,
		UserSN:   snStr,
		No:       data["no"],
		Nickname: data["nickname"],
		Settings: data["settings"],
	}, nil
}

// DelLoginSession removes a session from Redis (used on logout).
func (rs *RedisSessionStore) DelLoginSession(ctx context.Context, sessionID string) error {
	key := sessionKey(sessionID)
	return rs.client.Del(ctx, key).Err()
}

// RefreshTTL refreshes the TTL for an active session.
// Should be called on each authenticated request to keep the session alive.
func (rs *RedisSessionStore) RefreshTTL(ctx context.Context, sessionID string) error {
	key := sessionKey(sessionID)
	return rs.client.Expire(ctx, key, sessionTTL).Err()
}

// GCStats holds the result of a single session GC sweep.
type GCStats struct {
	ExpiredAnonymous int `json:"expired_anonymous"`
	ExpiredLoggedIn  int `json:"expired_logged_in"`
	OnlineUsers      int `json:"online_users"`
	AnonymousUsers   int `json:"anonymous_users"`
}

// gcStatsKey is the Redis key for the latest GC stats.
const gcStatsKey = "session:gc_stats"

// SetGCStats writes the latest GC sweep statistics to Redis.
func (rs *RedisSessionStore) SetGCStats(ctx context.Context, stats *GCStats) error {
	return rs.client.HSet(ctx, gcStatsKey,
		"expired_anonymous", stats.ExpiredAnonymous,
		"expired_logged_in", stats.ExpiredLoggedIn,
		"online_users", stats.OnlineUsers,
		"anonymous_users", stats.AnonymousUsers,
		"updated_at", time.Now().UTC().Format(time.RFC3339),
	).Err()
}
