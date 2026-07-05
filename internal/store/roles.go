package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	"BrainForever/infra/i18n"
)

// ============================================================
// Role struct
// ============================================================

// Role represents role information
type Role struct {
	ID       int64     `db:"id"`        // Auto-increment primary key
	RoleNo   int       `db:"role_no"`   // Role number (integer)
	RoleName string    `db:"role_name"` // Role name, max 60 characters
	UUID     string    `db:"uuid"`      // Unique user string (references users.uuid)
	IsPublic bool      `db:"is_public"` // Whether public, default false
	IsActive bool      `db:"is_active"` // Whether active
	CreateAt time.Time `db:"create_at"` // Creation time, defaults to current time
	UpdateAt time.Time `db:"update_at"` // Last update time, defaults to current time on creation, auto-updated on modification
}

// ============================================================
// RoleStore
// ============================================================

// RoleStore manages role storage
type RoleStore struct {
	db *sqlx.DB
}

// NewRoleStore creates a new RoleStore.
// dbPath is the path to user.db (e.g., "./user.db").
func NewRoleStore(dbPath string) (*RoleStore, error) {
	db, err := sqlx.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_open_role_db_failed"), err)
	}

	store := &RoleStore{db: db}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

// initSchema initializes the role table schema
func (s *RoleStore) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS roles (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			role_no   INTEGER NOT NULL,
			role_name TEXT    NOT NULL CHECK(length(role_name) <= 60),
			uuid      TEXT    NOT NULL REFERENCES users(uuid) ON DELETE CASCADE,
			is_public INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_roles_uuid
			ON roles(uuid);

		-- Automatically set update_at to current time when a row in roles is updated
		CREATE TRIGGER IF NOT EXISTS trg_roles_update_at
			BEFORE UPDATE ON roles
			FOR EACH ROW
		BEGIN
			UPDATE roles SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_init_role_table_failed"), err)
	}
	return nil
}

// ============================================================
// Role operations
// ============================================================

// CreateRole creates a new role.
// roleNo is the role number, roleName is the role name (max 60 chars), uuid is the unique user string.
func (s *RoleStore) CreateRole(roleNo int, roleName, uuid string, isPublic bool) (*Role, error) {
	if len(roleName) > 60 {
		return nil, fmt.Errorf("role name too long. max 60 characters, got %d", len(roleName))
	}
	if len(roleName) == 0 {
		return nil, fmt.Errorf("role name cannot be empty")
	}
	if len(uuid) == 0 {
		return nil, fmt.Errorf("uuid cannot be empty")
	}

	isPublicInt := 0
	if isPublic {
		isPublicInt = 1
	}

	result, err := s.db.Exec(
		"INSERT INTO roles(role_no, role_name, uuid, is_public) VALUES(?, ?, ?, ?)",
		roleNo, roleName, uuid, isPublicInt,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_create_role_failed"), err)
	}

	id, _ := result.LastInsertId()
	return s.GetRoleByID(id)
}

// GetRoleByID retrieves a role by ID
func (s *RoleStore) GetRoleByID(id int64) (*Role, error) {
	var r Role
	err := s.db.Get(&r, "SELECT id, role_no, role_name, uuid, is_public, is_active, create_at, update_at FROM roles WHERE id = ?", id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%s (id=%d)", i18n.T("db_role_not_found"), id)
		}
		return nil, fmt.Errorf("%s: %w", i18n.T("db_query_role_failed"), err)
	}
	return &r, nil
}

// ListRolesByUUID lists all roles for a given user
func (s *RoleStore) ListRolesByUUID(uuid string) ([]Role, error) {
	var roles []Role
	err := s.db.Select(&roles, "SELECT id, role_no, role_name, uuid, is_public, is_active, create_at, update_at FROM roles WHERE uuid = ? ORDER BY role_no", uuid)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_roles_failed"), err)
	}
	return roles, nil
}

// ListActiveRolesByUUID lists all active roles for a given user
func (s *RoleStore) ListActiveRolesByUUID(uuid string) ([]Role, error) {
	var roles []Role
	err := s.db.Select(&roles, "SELECT id, role_no, role_name, uuid, is_public, is_active, create_at, update_at FROM roles WHERE uuid = ? AND is_active = 1 ORDER BY role_no", uuid)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_active_roles_failed"), err)
	}
	return roles, nil
}

// UpdateRole updates role information (role number, role name, public status)
func (s *RoleStore) UpdateRole(id int64, roleNo int, roleName string, isPublic bool) error {
	if len(roleName) > 60 {
		return fmt.Errorf("role name too long. max 60 characters, got %d", len(roleName))
	}
	if len(roleName) == 0 {
		return fmt.Errorf("role name cannot be empty")
	}

	isPublicInt := 0
	if isPublic {
		isPublicInt = 1
	}

	result, err := s.db.Exec("UPDATE roles SET role_no = ?, role_name = ?, is_public = ? WHERE id = ?", roleNo, roleName, isPublicInt, id)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_update_role_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_role_not_found"), id)
	}
	return nil
}

// SetRoleActive sets the role's active status
func (s *RoleStore) SetRoleActive(id int64, active bool) error {
	activeInt := 0
	if active {
		activeInt = 1
	}
	result, err := s.db.Exec("UPDATE roles SET is_active = ? WHERE id = ?", activeInt, id)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_update_role_active_status_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_role_not_found"), id)
	}
	return nil
}

// DeleteRole deletes a role
func (s *RoleStore) DeleteRole(id int64) error {
	result, err := s.db.Exec("DELETE FROM roles WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_role_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_role_not_found"), id)
	}
	return nil
}

// Close closes the database connection
func (s *RoleStore) Close() error {
	return s.db.Close()
}
