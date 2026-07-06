package store

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
// userID and userSN are the authenticated user's identity.
// settingsJSON is the JSON-serialized UserSettings (API keys, theme, etc.).
func (rs *RedisSessionStore) SetLoginSession(ctx context.Context, sessionID string, userID int64, userSN string, settingsJSON string) error {
	key := sessionKey(sessionID)
	now := time.Now().UTC().Format(time.RFC3339)

	err := rs.client.HSet(ctx, key,
		"user_id", strconv.FormatInt(userID, 10),
		"user_sn", userSN,
		"settings", settingsJSON,
		"created_at", now,
		"last_active", now,
	).Err()
	if err != nil {
		return fmt.Errorf("redis: failed to set login session: %w", err)
	}

	// Set TTL so expired sessions are automatically cleaned up
	rs.client.Expire(ctx, key, sessionTTL)
	return nil
}

// GetLoginSession retrieves login state from Redis.
// Returns userID, userSN, settingsJSON, and a bool indicating if the session exists.
func (rs *RedisSessionStore) GetLoginSession(ctx context.Context, sessionID string) (userID int64, userSN string, settingsJSON string, ok bool, err error) {
	key := sessionKey(sessionID)

	data, err := rs.client.HGetAll(ctx, key).Result()
	if err != nil {
		return 0, "", "", false, fmt.Errorf("redis: failed to get login session: %w", err)
	}

	if len(data) == 0 {
		return 0, "", "", false, nil // session not found
	}

	uidStr, hasUID := data["user_id"]
	snStr, hasSN := data["user_sn"]
	settingsStr := data["settings"] // may be empty (old format sessions)
	if !hasUID || !hasSN {
		// Malformed entry — clean it up
		rs.client.Del(ctx, key)
		return 0, "", "", false, nil
	}

	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		rs.client.Del(ctx, key)
		return 0, "", "", false, nil
	}

	return uid, snStr, settingsStr, true, nil
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
	now := time.Now().UTC().Format(time.RFC3339)

	// Update last_active and refresh TTL
	pipe := rs.client.Pipeline()
	pipe.HSet(ctx, key, "last_active", now)
	pipe.Expire(ctx, key, sessionTTL)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis: failed to refresh session TTL: %w", err)
	}
	return nil
}
