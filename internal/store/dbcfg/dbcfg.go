// Package dbcfg provides global on-demand access to per-user SQLite databases.
// Uses package-level state (like theLogger pattern) to avoid dependency injection.
package dbcfg

import (
	"fmt"
	"os"
	"path/filepath"

	"BrainForever/infra/zylog"
	"BrainForever/internal/store"
)

// Package-level configuration, initialized once at startup.
var (
	theDBDir       string
	theEmbedderDim int
	theLogger      zylog.Logger
)

// InitDBConfig initializes the dbcfg package. Must be called once at startup.
func InitDBConfig(dbDir string, embedderDim int, logger zylog.Logger) {
	theDBDir = dbDir
	theEmbedderDim = embedderDim
	theLogger = logger
}

// dbPath returns: {dbDir}/{userSN}.{dbKind}.db
func dbPath(userSN string, dbKind string) string {
	return filepath.Join(theDBDir, userSN+"."+dbKind+".db")
}

// ensureDir creates the database directory if needed.
func ensureDir() error {
	return os.MkdirAll(theDBDir, 0755)
}

// ============================================================
// Chat database
// ============================================================

// InitLocalChatDB opens a user's chat database and ensures its schema exists.
// Use this when the database may not exist yet (e.g., first login/switchToUser).
// Caller MUST call CloseLocalChatDB when done.
func InitLocalChatDB(userSN string) (*store.ChatStore, error) {
	if err := ensureDir(); err != nil {
		return nil, fmt.Errorf("failed to create db dir %s: %w", theDBDir, err)
	}
	s, err := store.CreateLocalChatScheme(dbPath(userSN, "chats"))
	if err != nil {
		return nil, fmt.Errorf("failed to init chat db for %s: %w", userSN, err)
	}
	return s, nil
}

// OpenLocalChatDB opens a user's chat database WITHOUT schema initialization.
// Faster than InitLocalChatDB; used on hot paths where schema already exists.
// Caller MUST call CloseLocalChatDB when done.
func OpenLocalChatDB(userSN string) (*store.ChatStore, error) {
	if err := ensureDir(); err != nil {
		return nil, fmt.Errorf("failed to create db dir %s: %w", theDBDir, err)
	}
	s, err := store.OpenChatStore(dbPath(userSN, "chats"))
	if err != nil {
		return nil, fmt.Errorf("failed to open chat db for %s: %w", userSN, err)
	}
	return s, nil
}

// CloseLocalChatDB closes a chat database. Nil-safe.
func CloseLocalChatDB(s *store.ChatStore) {
	if s != nil {
		s.Close()
	}
}

// ============================================================
// Brain database
// ============================================================

// InitLocalBrainDB opens a user's brain database and ensures its schema exists.
// Use this when the database may not exist yet (e.g., first login).
// Caller MUST call CloseLocalBrainDB when done.
func InitLocalBrainDB(userSN string) (*store.BrainStore, error) {
	if err := ensureDir(); err != nil {
		return nil, fmt.Errorf("failed to create db dir %s: %w", theDBDir, err)
	}
	s, err := store.NewBrainStore(dbPath(userSN, "brain"), theEmbedderDim, theLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to init brain db for %s: %w", userSN, err)
	}
	return s, nil
}

// OpenLocalBrainDB opens a user's brain database WITHOUT schema initialization.
// Faster than InitLocalBrainDB; used on hot paths where schema already exists.
// Caller MUST call CloseLocalBrainDB when done.
func OpenLocalBrainDB(userSN string) (*store.BrainStore, error) {
	if err := ensureDir(); err != nil {
		return nil, fmt.Errorf("failed to create db dir %s: %w", theDBDir, err)
	}
	s, err := store.OpenBrainStore(dbPath(userSN, "brain"), theEmbedderDim, theLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to open brain db for %s: %w", userSN, err)
	}
	return s, nil
}

// CloseLocalBrainDB closes a brain database. Nil-safe.
func CloseLocalBrainDB(s *store.BrainStore) {
	if s != nil {
		s.Close()
	}
}
