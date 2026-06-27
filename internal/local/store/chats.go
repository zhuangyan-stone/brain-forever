package store

import (
	"fmt"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type ChatStore struct {
	db *sqlx.DB
}

/*
	User conversation records
*/

// Chat represents a single conversation (a discussion) in the database.
type Chat struct {
	ID int64  `db:"id" json:"id"` // Auto-increment ID
	SN string `db:"sn" json:"sn"` // Globally unique SN string

	RoleNo int `db:"role_no" json:"role_no"` // Role / personality number

	Title      string `db:"title" json:"title"`             // Conversation title
	TitleState int8   `db:"title_state" json:"title_state"` // Title modification state: 0=original (default), 1=AI modified, 2=user modified

	ExtractMode int8       `db:"extract_mode" json:"extract_mode"` // 0=manual, 1=auto, default 0
	ExtractedAt *time.Time `db:"trait_time" json:"extracted_at"`   // Last extraction time, default null

	ExtractedCount int  `db:"extracted_count" json:"extracted_count"` // Number of traits extracted for this chat, default 0
	Deleted        bool `db:"deleted" json:"-"`                       // Soft delete flag (excluded from JSON)
	Pinned         bool `db:"pinned" json:"pinned"`                   // Whether pinned
	Category       int  `db:"category" json:"category"`               // Category ID, 0=uncategorized

	CreateAt time.Time `db:"create_at" json:"create_at"`
	UpdateAt time.Time `db:"update_at" json:"update_at"`
}

type Message struct {
	ID     int64 `db:"id"`      // Auto-increment ID
	ChatID int64 `db:"chat_id"` // Belonging chat ID

	GroupIndex int  `db:"group_index"` // Message group index
	Role       int8 `db:"role"`        // 0: user 1: assistant

	Reasoning *string `db:"reasoning"`
	Content   string  `db:"content"` // Content

	Extracted   bool `db:"extracted"`   // Whether extracted, default 0
	Interrupted int  `db:"interrupted"` // 0=done, 1=user-interrupted, 2=backend-error

	CreateAt time.Time `db:"create_at"`
	UpdateAt time.Time `db:"update_at"`
}

// WebSource represents a web search result source stored in the database.
// This is the store-layer equivalent of toolimp.WebSource, defined separately
// to avoid circular dependencies between store and agent packages.
type WebSource struct {
	ID          int64     `db:"id"`      // Auto-increment primary key
	ChatID      int64     `db:"chat_id"` // References chat_sessions.id
	MsgID       int64     `db:"msg_id"`  // Message group index (= agent.Message.ID)
	Title       string    `db:"title"`
	Content     string    `db:"content"`
	URL         string    `db:"url"`
	SiteName    string    `db:"site_name"`
	SiteIcon    string    `db:"site_icon"`
	PublishDate string    `db:"publish_date"`
	Score       float64   `db:"score"`
	CreateAt    time.Time `db:"create_at"`
}

func CreateLocalChatScheme(dbFile string) (*ChatStore, error) {
	// Check if the database specified by dbFile (path + filename) exists.
	// If not, create it. Contains two tables: chat_sessions and chat_messages,
	// corresponding to the two structs above.

	// Check if the database file exists
	_, err := os.Stat(dbFile)
	if os.IsNotExist(err) {
		// File doesn't exist, create an empty file to ensure sqlx.Open works
		f, err := os.Create(dbFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create chat database file. %w", err)
		}
		f.Close()
	}

	// Open the database (WAL mode for better concurrent performance)
	db, err := sqlx.Open("sqlite3", dbFile+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open chat database. %w", err)
	}

	store := &ChatStore{db: db}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

// initSchema initializes chat-related table schemas.
func (s *ChatStore) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			sn        TEXT    NOT NULL UNIQUE,
			role_no   INTEGER NOT NULL,
			title     TEXT    NOT NULL DEFAULT '',
			title_state INTEGER NOT NULL DEFAULT 0,
			extract_mode       INTEGER NOT NULL DEFAULT 0,
			trait_time         DATETIME,
			extracted_count    INTEGER NOT NULL DEFAULT 0,
			deleted   INTEGER NOT NULL DEFAULT 0,
			pinned    INTEGER NOT NULL DEFAULT 0,
			category  INTEGER NOT NULL DEFAULT 0,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS chat_messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id    INTEGER NOT NULL REFERENCES chat_sessions(id),
			group_index INTEGER NOT NULL,
			role       INTEGER NOT NULL,
			reasoning    TEXT,
			content    TEXT    NOT NULL,
			extracted  INTEGER NOT NULL DEFAULT 0,
			interrupted INTEGER NOT NULL DEFAULT 0,
			create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS web_sources (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id      INTEGER NOT NULL REFERENCES chat_sessions(id),
			msg_id       INTEGER NOT NULL,
			title        TEXT    NOT NULL DEFAULT '',
			content      TEXT    NOT NULL DEFAULT '',
			url          TEXT    NOT NULL DEFAULT '',
			site_name    TEXT    NOT NULL DEFAULT '',
			site_icon    TEXT    NOT NULL DEFAULT '',
			publish_date TEXT    NOT NULL DEFAULT '',
			score        REAL    NOT NULL DEFAULT 0,
			create_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_web_sources_chat_msg
			ON web_sources(chat_id, msg_id);

		CREATE TRIGGER IF NOT EXISTS trg_chat_sessions_update_at
			BEFORE UPDATE ON chat_sessions
			FOR EACH ROW
		BEGIN
			UPDATE chat_sessions SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;

		CREATE TRIGGER IF NOT EXISTS trg_chat_messages_update_at
			BEFORE UPDATE ON chat_messages
			FOR EACH ROW
		BEGIN
			UPDATE chat_messages SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to initialize chat tables. %w", err)
	}

	// Migration: rename extracted_message_count to extracted_count for existing databases
	// (SQLite 3.25.0+ supports RENAME COLUMN; the mattn/go-sqlite3 driver bundles a recent version)
	var colName string
	err = s.db.QueryRow(
		`SELECT name FROM pragma_table_info('chat_sessions') WHERE name = 'extracted_message_count'`,
	).Scan(&colName)
	if err == nil {
		// Old column exists, rename it
		if _, err := s.db.Exec(`ALTER TABLE chat_sessions RENAME COLUMN extracted_message_count TO extracted_count`); err != nil {
			// Fallback: add new column if rename fails
			s.db.Exec(`ALTER TABLE chat_sessions ADD COLUMN extracted_count INTEGER NOT NULL DEFAULT 0`)
			s.db.Exec(`UPDATE chat_sessions SET extracted_count = extracted_message_count`)
		}
	}

	return nil
}

// InsertChat creates a new chat session and returns it.
func (s *ChatStore) InsertChat(sn string, roleNO int,
	title string, extractMode int8) (*Chat, error) {

	result, err := s.db.Exec(
		`INSERT INTO chat_sessions(sn, role_no, title, extract_mode)
		 VALUES(?, ?, ?, ?)`,
		sn, roleNO, title, extractMode,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert chat. %w", err)
	}

	id, _ := result.LastInsertId()

	var chat Chat
	err = s.db.Get(&chat,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        trait_time, extracted_count,
		        deleted, pinned, category, create_at, update_at
		 FROM chat_sessions WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query inserted chat. %w", err)
	}
	return &chat, nil
}

// LogicDelete soft-deletes the session identified by SN by setting deleted to true.
func (s *ChatStore) LogicDelete(sn string) error {
	result, err := s.db.Exec(
		"UPDATE chat_sessions SET deleted = 1 WHERE sn = ?",
		sn,
	)
	if err != nil {
		return fmt.Errorf("failed to logic delete session. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (sn=%s)", sn)
	}
	return nil
}

// PhysicalDelete physically deletes the session identified by id + sn.
// Also deletes all its messages (transaction-safe).
func (s *ChatStore) PhysicalDelete(id int, sn string) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction. %w", err)
	}
	defer tx.Rollback()

	// First check if the session exists, ensuring id + sn match
	var exists bool
	err = tx.Get(&exists,
		"SELECT COUNT(1) FROM chat_sessions WHERE id = ? AND sn = ?",
		id, sn,
	)
	if err != nil {
		return fmt.Errorf("failed to check session existence. %w", err)
	}
	if !exists {
		return fmt.Errorf("session not found (id=%d, sn=%s)", id, sn)
	}

	// Delete all web sources under this session
	_, err = tx.Exec(
		"DELETE FROM web_sources WHERE chat_id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to delete web sources of session. %w", err)
	}

	// Delete all messages under this session
	_, err = tx.Exec(
		"DELETE FROM chat_messages WHERE chat_id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to delete messages of session. %w", err)
	}

	// Delete the session itself
	_, err = tx.Exec(
		"DELETE FROM chat_sessions WHERE id = ? AND sn = ?",
		id, sn,
	)
	if err != nil {
		return fmt.Errorf("failed to physical delete session. %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction. %w", err)
	}
	return nil
}

// FindChatBySN finds a chat by its SN (regardless of deleted status).
func (s *ChatStore) FindChatBySN(sn string) (*Chat, error) {
	var chat Chat
	err := s.db.Get(&chat,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        trait_time, extracted_count,
		        deleted, pinned, category, create_at, update_at
		 FROM chat_sessions WHERE sn = ?`, sn,
	)
	if err != nil {
		return nil, fmt.Errorf("chat not found (sn=%s): %w", sn, err)
	}
	return &chat, nil
}

// ListDeletedChats lists the most recent N deleted chat records, ordered by update_at descending.
func (s *ChatStore) ListDeletedChats(n int) ([]Chat, error) {
	var chats []Chat
	err := s.db.Select(&chats,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		    trait_time, extracted_count,
		    deleted, pinned, category, create_at, update_at
		 FROM chat_sessions
		 WHERE deleted = 1
		 ORDER BY update_at DESC
		 LIMIT ?`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list deleted chats. %w", err)
	}
	return chats, nil
}

// RestoreChat restores a soft-deleted chat by setting deleted = 0.
func (s *ChatStore) RestoreChat(sn string) error {
	result, err := s.db.Exec(
		"UPDATE chat_sessions SET deleted = 0 WHERE sn = ?",
		sn,
	)
	if err != nil {
		return fmt.Errorf("failed to restore chat. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("chat not found (sn=%s)", sn)
	}
	return nil
}

// ListChats lists the most recent N non-deleted chat records, ordered by pinned first, then create_at descending.
func (s *ChatStore) ListChats(n int) ([]Chat, error) {
	var chats []Chat
	err := s.db.Select(&chats,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        trait_time, extracted_count,
		        deleted, pinned, category, create_at, update_at
		 FROM chat_sessions
		 WHERE deleted = 0
		 ORDER BY pinned DESC, create_at DESC
		 LIMIT ?`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list chats. %w", err)
	}
	return chats, nil
}

// UpdateChatTitle updates the chat title and title state.
func (s *ChatStore) UpdateChatTitle(id int64, title string, titleState int8) error {
	result, err := s.db.Exec(
		"UPDATE chat_sessions SET title = ?, title_state = ? WHERE id = ?",
		title, titleState, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update chat title. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("chat not found (id=%d)", id)
	}
	return nil
}

// UpdateChatPin updates the pinned state of a chat.
func (s *ChatStore) UpdateChatPin(id int64, pinned bool) error {
	pinVal := 0
	if pinned {
		pinVal = 1
	}
	result, err := s.db.Exec(
		"UPDATE chat_sessions SET pinned = ? WHERE id = ?",
		pinVal, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update chat pin. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("chat not found (id=%d)", id)
	}
	return nil
}

// EmptyTrash permanently deletes all soft-deleted chats (and their messages/web_sources).
func (s *ChatStore) EmptyTrash() error {
	// Get all deleted chat IDs first
	var deletedIDs []int64
	err := s.db.Select(&deletedIDs,
		"SELECT id FROM chat_sessions WHERE deleted = 1",
	)
	if err != nil {
		return fmt.Errorf("failed to query deleted chats: %w", err)
	}

	if len(deletedIDs) == 0 {
		return nil
	}

	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, id := range deletedIDs {
		// Delete all web sources under this session
		_, err = tx.Exec("DELETE FROM web_sources WHERE chat_id = ?", id)
		if err != nil {
			return fmt.Errorf("failed to delete web sources for chat %d: %w", id, err)
		}

		// Delete all messages under this session
		_, err = tx.Exec("DELETE FROM chat_messages WHERE chat_id = ?", id)
		if err != nil {
			return fmt.Errorf("failed to delete messages for chat %d: %w", id, err)
		}
	}

	// Delete all deleted sessions
	_, err = tx.Exec("DELETE FROM chat_sessions WHERE deleted = 1")
	if err != nil {
		return fmt.Errorf("failed to delete trashed sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// ============================================================
// Extraction progress management
// ============================================================

// UpdateExtractionProgress updates the trait extraction progress for a chat.
// Sets trait_time to now() and adds to the extracted trait count.
// The increment parameter is the number of newly extracted traits in this round.
func (s *ChatStore) UpdateExtractionProgress(chatID int64, increment int) error {
	result, err := s.db.Exec(
		`UPDATE chat_sessions
		 SET trait_time = CURRENT_TIMESTAMP,
		     extracted_count = extracted_count + ?
		 WHERE id = ?`,
		increment, chatID,
	)
	if err != nil {
		return fmt.Errorf("failed to update extraction progress: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("chat not found (id=%d)", chatID)
	}
	return nil
}

// MarkMessagesExtracted marks all messages in a chat up to (and including) the
// given message id as extracted (extracted = 1). This is called after a
// successful trait extraction to record which messages have been processed.
// Using message id (auto-increment PK) as the cutoff ensures that even if
// group_index is non-contiguous, all messages up to the given point are marked.
func (s *ChatStore) MarkMessagesExtracted(chatID int64, upToID int64) error {
	_, err := s.db.Exec(
		`UPDATE chat_messages
		 SET extracted = 1
		 WHERE chat_id = ? AND id <= ? AND extracted = 0`,
		chatID, upToID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark messages as extracted: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *ChatStore) Close() error {
	return s.db.Close()
}

// TouchChat updates the update_at timestamp of a chat session to the current time.
// This is used when a new message is inserted, so the chat moves to the top
// of the list when ordered by update_at DESC (e.g., in ListChats).
func (s *ChatStore) TouchChat(id int64) error {
	result, err := s.db.Exec(
		"UPDATE chat_sessions SET update_at = CURRENT_TIMESTAMP WHERE id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to touch chat update_at: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("chat not found (id=%d)", id)
	}
	return nil
}
