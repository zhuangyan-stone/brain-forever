package store

import (
	"fmt"

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

// ClosePGDB closes the global PostgreSQL database connection.
// Should be called during graceful shutdown.
func ClosePGDB() error {
	if thePGDBC != nil {
		return thePGDBC.Close()
	}
	return nil
}
