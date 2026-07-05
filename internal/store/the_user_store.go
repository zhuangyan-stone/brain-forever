package store

import "path/filepath"

var (
	theUserStore *UserStore
	theDBDir     string // database directory, used by Login/Logout
)

// TheUserStore returns the global UserStore singleton.
// Panics if InitTheUserStore has not been called.
func TheUserStore() *UserStore {
	if theUserStore == nil {
		panic("TheUserStore is nil - call InitTheUserStore first")
	}
	return theUserStore
}

// InitTheUserStore opens (or creates) users.db and initializes its schema.
// dbDir is the directory for database files, e.g. "localdb".
// Opens before HTTP server starts listening.
func InitTheUserStore(dbDir string) error {
	s, err := OpenUserStore(filepath.Join(dbDir, "users.db"))
	if err != nil {
		return err
	}
	theDBDir = dbDir
	theUserStore = s
	return nil
}

// CloseTheUserStore closes the global UserStore.
// Called after HTTP server stops listening (via defer in main).
func CloseTheUserStore() {
	if theUserStore != nil {
		theUserStore.Close()
	}
}
