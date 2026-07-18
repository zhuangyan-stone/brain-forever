package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // Register pgx driver for sqlx
	"github.com/jmoiron/sqlx"

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

// InitSchema initializes all database schemas by reading a SQL file from
// bin/settings/init_sql/. This directory must contain exactly zero or one .sql
// file — if multiple are found, the function returns an error to prevent
// accidental double-execution.
//
// After successful execution, the SQL file is automatically renamed with a "-"
// prefix so that it is skipped on subsequent restarts.
//
// Must be called after InitPGDB. The dimension parameter is used to replace
// the {dimension} placeholder in VECTOR({dimension}) column definitions.
//
// The returned string is the name of the executed SQL file, or empty if none.
func InitSchema(dimension int) (string, error) {
	const sqlDir = "bin/settings/init_sql"

	entries, err := os.ReadDir(sqlDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read directory %s. %w", sqlDir, err)
	}

	// Collect .sql files from the directory, skipping files prefixed with "-"
	// (those are considered disabled/skipped).
	var sqlFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "-") {
			continue
		}
		if strings.HasSuffix(name, ".sql") {
			sqlFiles = append(sqlFiles, name)
		}
	}

	// Must have exactly zero or one SQL file
	if len(sqlFiles) == 0 {
		return "", nil
	}
	if len(sqlFiles) > 1 {
		return "", fmt.Errorf("found %d SQL files in %s, expected at most one", len(sqlFiles), sqlDir)
	}

	sqlPath := filepath.Join(sqlDir, sqlFiles[0])
	schemaBytes, err := os.ReadFile(sqlPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s. %w", sqlPath, err)
	}

	// Replace {dimension} placeholder with the actual vector dimension
	schema := strings.ReplaceAll(string(schemaBytes), "{dimension}", fmt.Sprintf("%d", dimension))

	// Verify pgvector extension is already installed (must be created by DBA beforehand)
	var extExists int
	if err := ThePGDB().Get(&extExists, "SELECT 1 FROM pg_extension WHERE extname = 'vector'"); err != nil {
		return "", fmt.Errorf("pgvector extension is not installed. run 'CREATE EXTENSION vector' as a superuser first. %w", err)
	}

	// Execute the full schema
	if _, err := ThePGDB().Exec(schema); err != nil {
		return "", fmt.Errorf("failed to execute %s. %w", sqlFiles[0], err)
	}

	// Rename the executed SQL file with a "-" prefix so it is skipped on restart
	newPath := filepath.Join(sqlDir, "-"+sqlFiles[0])
	if err := os.Rename(sqlPath, newPath); err != nil {
		return "", fmt.Errorf("failed to rename executed SQL file %s. %w", sqlPath, err)
	}

	return sqlFiles[0], nil
}

// ClosePGDB closes the global PostgreSQL database connection.
// Should be called during graceful shutdown.
func ClosePGDB() error {
	if thePGDBC != nil {
		return thePGDBC.Close()
	}
	return nil
}
