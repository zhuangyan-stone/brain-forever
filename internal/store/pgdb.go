package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // Register pgx driver for sqlx
	"github.com/jmoiron/sqlx"

	"BrainForever/infra/zylog"
	"BrainForever/internal/config"
)

// ============================================================
// Global PostgreSQL database connection singleton
//
// All stores should use ThePGDB() to obtain the connection.
// ============================================================

var thePGDBC *sqlx.DB

// InitPGDB initializes the global PostgreSQL database connection.
// cfg provides the DSN and connection pool settings.
// Must be called once at startup, before any store that uses PostgreSQL.
func InitPGDB(cfg *config.DatabaseConfig) error {
	db, err := sqlx.Open("pgx", cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open PostgreSQL connection. %w", err)
	}

	// Verify the connection is actually reachable
	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping PostgreSQL. %w", err)
	}

	// Set session timezone to UTC for consistent NOW() behavior
	if _, err := db.Exec("SET timezone TO 'UTC'"); err != nil {
		db.Close()
		return fmt.Errorf("failed to set timezone to UTC. %w", err)
	}

	// Configure connection pool from config
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	thePGDBC = db
	return nil
}

// ThePGDB returns the global PostgreSQL database connection.
// Panics if InitPGDB has not been called.
func ThePGDB() *sqlx.DB {
	if thePGDBC == nil {
		panic("PostgreSQL DB not initialized - call InitPGDB first")
	}
	return thePGDBC
}

// validSQLPrefix matches files named like "000.init.sql", "001.migration.sql" etc.
// Files must start with exactly three digits followed by a dot.
var validSQLPrefix = regexp.MustCompile(`^\d{3}\.`)

// InitSchema scans bin/settings/init_sql/ for all valid SQL files, sorts them by
// name (by their XXX. prefix), and executes each one in order.
//
// A valid SQL file must:
//   - Have a .sql extension
//   - NOT be prefixed with "-" (that marks it as already executed/skipped)
//   - Start with "XXX." where XXX is exactly three digits (e.g., "000.", "001.")
//
// After successful execution, each file is renamed with a "-" prefix so it is
// skipped on subsequent restarts. If any file fails, an error is logged and
// returned; the caller should Fatal on error.
//
// Must be called after InitPGDB. The dimension parameter is used to replace
// the {dimension} placeholder in VECTOR({dimension}) column definitions.
//
// Logging is done via the given logger, wrapped with a "sql-init" subject tag:
//   - Debug: full list of pending SQL files
//   - Info:  each file after successful execution
//   - Error: file name + SQL snippet + error on failure (then the error is returned)
func InitSchema(logger zylog.Logger, dimension int) error {
	const sqlDir = "bin/settings/init_sql"
	log := zylog.WrapWithSubject(logger, "sql-init")

	entries, err := os.ReadDir(sqlDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read directory %s. %w", sqlDir, err)
	}

	// Collect .sql files that are valid: not prefixed with "-", and match "XXX." pattern.
	var sqlFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "-") {
			continue
		}
		if strings.HasSuffix(name, ".sql") && validSQLPrefix.MatchString(name) {
			sqlFiles = append(sqlFiles, name)
		}
	}

	if len(sqlFiles) == 0 {
		return nil
	}

	// Sort by name — lexicographic order on the "XXX." prefix works naturally
	// (000 < 001 < 002 < ... < 999).
	sort.Strings(sqlFiles)

	// Debug: log the full list of pending SQL files
	log.Debugf("pending SQL files: %s", strings.Join(sqlFiles, ", "))

	// Verify pgvector extension is already installed (must be created by DBA beforehand)
	var extExists int
	if err := ThePGDB().Get(&extExists, "SELECT 1 FROM pg_extension WHERE extname = 'vector'"); err != nil {
		return fmt.Errorf("pgvector extension is not installed. run 'CREATE EXTENSION vector' as a superuser first. %w", err)
	}

	// Execute each SQL file in order, renaming on success.
	for _, fileName := range sqlFiles {
		sqlPath := filepath.Join(sqlDir, fileName)

		schemaBytes, err := os.ReadFile(sqlPath)
		if err != nil {
			return fmt.Errorf("failed to read %s. %w", sqlPath, err)
		}

		// Replace {dimension} placeholder with the actual vector dimension
		schema := strings.ReplaceAll(string(schemaBytes), "{dimension}", fmt.Sprintf("%d", dimension))

		if _, err := ThePGDB().Exec(schema); err != nil {
			// Include a snippet of the SQL content for easier debugging
			snippet := firstN(string(schemaBytes), 200)
			log.Errorf("failed to execute %s.\nsnippet: %s\n%v", fileName, snippet, err)
			return fmt.Errorf("failed to execute %s. %w", fileName, err)
		}

		// Rename the executed SQL file with a "-" prefix so it is skipped on restart
		newPath := filepath.Join(sqlDir, "-"+fileName)
		if err := os.Rename(sqlPath, newPath); err != nil {
			log.Errorf("failed to rename executed SQL file %s. %v", sqlPath, err)
			return fmt.Errorf("failed to rename executed SQL file %s. %w", sqlPath, err)
		}

		log.Infof("executed: %s", fileName)
	}

	return nil
}

// firstN returns the first n runes of s, appending "..." if truncated.
func firstN(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// ClosePGDB closes the global PostgreSQL database connection.
// Should be called during graceful shutdown.
func ClosePGDB() error {
	if thePGDBC != nil {
		return thePGDBC.Close()
	}
	return nil
}
