// Package dbpath provides unified per-user database file path construction.
// Both store and dbc packages import this to ensure consistent path logic.
package dbpath

import (
	"fmt"
	"path/filepath"
)

// DirID returns the 4-digit subdirectory name for a user ID.
// e.g. ID=1 => "0000", ID=1000 => "0001", ID=12345 => "0012"
func DirID(userID int64) string {
	return fmt.Sprintf("%04d", userID/1000)
}

// ForUser returns: {dbDir}/{DirID(userID)}/{userSN}.{dbKind}.db
func ForUser(dbDir string, userID int64, userSN string, dbKind string) string {
	return filepath.Join(dbDir, DirID(userID), userSN+"."+dbKind+".db")
}
