package store

import (
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // Register pgx driver for sqlx
	"github.com/jmoiron/sqlx"
)

// ============================================================
// Global PostgreSQL database connection singleton
//
// All stores should use ThePGDB() to obtain the connection.
// ============================================================

var thePGDBC *sqlx.DB

// InitPGDB initializes the global PostgreSQL database connection.
// dsn is the PostgreSQL connection URI, e.g.
//
//	"postgres://user:password@127.0.0.1:5432/d2brain?sslmode=disable"
//
// Must be called once at startup, before any store that uses PostgreSQL.
func InitPGDB(dsn string) error {
	db, err := sqlx.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("failed to open PostgreSQL connection: %w", err)
	}

	// Verify the connection is actually reachable
	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	// Set session timezone to UTC for consistent NOW() behavior
	if _, err := db.Exec("SET timezone TO 'UTC'"); err != nil {
		db.Close()
		return fmt.Errorf("failed to set timezone to UTC: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

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

// InitSchema initializes all database schemas by reading bin/settings/init.sql.
// This is the single entry point for schema initialization — replaces all
// per-store EnsureSchema() calls.
//
// Must be called after InitPGDB. The dimension parameter is used to replace
// the {dimension} placeholder in VECTOR({dimension}) column definitions.
func InitSchema(dimension int) error {
	const initSQLPath = "bin/settings/init.sql"
	schemaBytes, err := os.ReadFile(initSQLPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("init.sql not found at %s", initSQLPath)
		}
		return fmt.Errorf("failed to read %s: %w", initSQLPath, err)
	}

	// Replace {dimension} placeholder with the actual vector dimension
	schema := strings.ReplaceAll(string(schemaBytes), "{dimension}", fmt.Sprintf("%d", dimension))

	// Ensure pgvector extension is installed (idempotent)
	if _, err := ThePGDB().Exec("CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("failed to create pgvector extension: %w", err)
	}

	// Execute the full schema
	if _, err := ThePGDB().Exec(schema); err != nil {
		return fmt.Errorf("failed to execute init.sql: %w", err)
	}

	return nil
}

// ClosePGDB closes the global PostgreSQL database connection.
// Should be called during graceful shutdown.
func ClosePGDB() error {
	if thePGDBC != nil {
		return thePGDBC.Close()
	}
	return nil
}
