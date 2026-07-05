// Package dbcfg provides global on-demand access to per-user SQLite databases.
// Uses package-level state (like theLogger pattern) to avoid dependency injection.
package dbc

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

// dirID returns the 4-digit subdirectory name for a user ID.
// e.g. ID=1 => "0000", ID=1000 => "0001", ID=12345 => "0012"
func dirID(userID int64) string {
	return fmt.Sprintf("%04d", userID/1000)
}

// dbPath returns: {dbDir}/{dirID}/{userSN}.{dbKind}.db
func dbPath(userID int64, userSN string, dbKind string) string {
	return filepath.Join(theDBDir, dirID(userID), userSN+"."+dbKind+".db")
}

// ensureUserDBDir creates the user's subdirectory if needed.
func ensureUserDBDir(userID int64) error {
	return os.MkdirAll(filepath.Join(theDBDir, dirID(userID)), 0755)
}

// ============================================================
// InitUserDB — first-time initialization
// ============================================================

// InitUserDB ensures both chat and brain databases exist with schema.
// Called once during login. Caller MUST close both returned stores when done.
func InitUserDB(userID int64, userSN string) (chatStore *store.ChatStore, brainStore *store.BrainStore, err error) {
	if err := ensureUserDBDir(userID); err != nil {
		return nil, nil, fmt.Errorf("failed to create db dir for user %d: %w", userID, err)
	}

	chatStore, err = initLocalChatDB(userID, userSN)
	if err != nil {
		return nil, nil, err
	}

	brainStore, err = initLocalBrainDB(userID, userSN)
	if err != nil {
		chatStore.Close()
		return nil, nil, err
	}

	return chatStore, brainStore, nil
}

// initLocalChatDB opens a user's chat database and ensures its schema exists.
func initLocalChatDB(userID int64, userSN string) (*store.ChatStore, error) {
	s, err := store.CreateLocalChat(dbPath(userID, userSN, "chats"))
	if err != nil {
		return nil, fmt.Errorf("failed to init chat db for %s: %w", userSN, err)
	}
	return s, nil
}

// initLocalBrainDB opens a user's brain database and ensures its schema exists.
func initLocalBrainDB(userID int64, userSN string) (*store.BrainStore, error) {
	s, err := store.NewBrainStore(dbPath(userID, userSN, "brain"), theEmbedderDim, theLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to init brain db for %s: %w", userSN, err)
	}
	return s, nil
}

// ============================================================
// Chat database
// ============================================================

// OpenLocalChatDB opens a user's chat database WITHOUT schema initialization.
// Assumes the user's DB directory already exists (ensured by InitUserDB during login).
// Used on hot paths where schema already exists. Caller MUST call CloseLocalChatDB when done.
func OpenLocalChatDB(userID int64, userSN string) (*store.ChatStore, error) {
	s, err := store.OpenChatStore(dbPath(userID, userSN, "chats"))
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

// OpenLocalBrainDB opens a user's brain database WITHOUT schema initialization.
// Assumes the user's DB directory already exists (ensured by InitUserDB during login).
// Used on hot paths where schema already exists. Caller MUST call CloseLocalBrainDB when done.
func OpenLocalBrainDB(userID int64, userSN string) (*store.BrainStore, error) {
	s, err := store.OpenBrainStore(dbPath(userID, userSN, "brain"), theEmbedderDim, theLogger)
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
