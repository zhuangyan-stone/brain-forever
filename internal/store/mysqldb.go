package store

import (
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// ============================================================
// Global MySQL database connection singleton
//
// All stores that need MySQL access (UserStore, future PaymentStore, etc.)
// should use TheMySQLDB() to obtain the connection.
// ============================================================

var theMySQLDBC *sqlx.DB

// InitMySQLDB initializes the global MySQL database connection.
// dsn is the MySQL data source name, e.g.
//
//	"user:password@tcp(127.0.0.1:3306)/brain_forever"
//
// Must be called once at startup, before any store that uses MySQL.
func InitMySQLDB(dsn string) error {
	// Ensure parseTime=true for sqlx to scan DATETIME into time.Time
	db, err := sqlx.Open("mysql", dsn+"?charset=utf8mb4&parseTime=true&loc=Local")
	if err != nil {
		return fmt.Errorf("failed to open MySQL connection: %w", err)
	}

	// Verify the connection is actually reachable
	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping MySQL: %w", err)
	}

	theMySQLDBC = db
	return nil
}

// TheMySQLDB returns the global MySQL database connection.
// Panics if InitMySQLDB has not been called.
func TheMySQLDB() *sqlx.DB {
	if theMySQLDBC == nil {
		panic("MySQL DB not initialized - call InitMySQLDB first")
	}
	return theMySQLDBC
}

// CloseMySQLDB closes the global MySQL database connection.
// Should be called during graceful shutdown.
func CloseMySQLDB() error {
	if theMySQLDBC != nil {
		return theMySQLDBC.Close()
	}
	return nil
}
