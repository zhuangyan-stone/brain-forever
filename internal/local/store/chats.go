package store

import (
	"fmt"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	"BrainForever/infra/i18n"
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

	ExtractMode    int8       `db:"extract_mode" json:"extract_mode"`       // 0=manual, 1=auto, default 0
	ExtractedAt    *time.Time `db:"extracted_at" json:"extracted_at"`       // Last extraction time, default null
	ExtractedCount int        `db:"extracted_count" json:"extracted_count"` // Number of traits extracted for this chat, default 0

	Deleted bool `db:"deleted" json:"-"`     // Soft delete flag (excluded from JSON)
	Pinned  bool `db:"pinned" json:"pinned"` // Whether pinned
	Taged   bool `db:"taged" json:"taged"`   // Whether tagged/classified

	CreateAt time.Time `db:"create_at" json:"create_at"`
	UpdateAt time.Time `db:"update_at" json:"update_at"`
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
			return nil, fmt.Errorf("%s: %w", i18n.T("db_create_chat_db_file_failed"), err)
		}
		f.Close()
	}

	// Open the database (WAL mode for better concurrent performance)
	db, err := sqlx.Open("sqlite3", dbFile+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_open_chat_db_failed"), err)
	}

	store := &ChatStore{db: db}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
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
		return nil, fmt.Errorf("%s: %w", i18n.T("db_insert_chat_failed"), err)
	}

	id, _ := result.LastInsertId()

	var chat Chat
	err = s.db.Get(&chat,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_query_inserted_chat_failed"), err)
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
		return fmt.Errorf("%s: %w", i18n.T("db_logic_delete_session_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (sn=%s)", i18n.T("db_session_not_found"), sn)
	}
	return nil
}

// PhysicalDelete physically deletes the session identified by id + sn.
// Also deletes all its messages, web sources, and tags (transaction-safe).
// Uses id (chat_id) for related table deletes since they now reference chat_sessions(id).
func (s *ChatStore) PhysicalDelete(id int, sn string) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_begin_transaction_failed"), err)
	}
	defer tx.Rollback()

	// First check if the session exists, ensuring id + sn match
	var exists bool
	err = tx.Get(&exists,
		"SELECT COUNT(1) FROM chat_sessions WHERE id = ? AND sn = ?",
		id, sn,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_check_session_existence_failed"), err)
	}
	if !exists {
		return fmt.Errorf("%s (id=%d, sn=%s)", i18n.T("db_session_not_found"), id, sn)
	}

	// Delete all tags under this session (using id)
	_, err = tx.Exec(
		"DELETE FROM chat_tags WHERE chat_id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_tags_of_session_failed"), err)
	}

	// Delete all web sources under this session (using id)
	_, err = tx.Exec(
		"DELETE FROM web_sources WHERE chat_id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_web_sources_of_session_failed"), err)
	}

	// Delete all messages under this session (using id)
	_, err = tx.Exec(
		"DELETE FROM chat_messages WHERE chat_id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_messages_of_session_failed"), err)
	}

	// Delete the session itself
	_, err = tx.Exec(
		"DELETE FROM chat_sessions WHERE id = ? AND sn = ?",
		id, sn,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_physical_delete_session_failed"), err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_commit_transaction_failed"), err)
	}
	return nil
}

// FindChatBySN finds a chat by its SN (regardless of deleted status).
func (s *ChatStore) FindChatBySN(sn string) (*Chat, error) {
	var chat Chat
	err := s.db.Get(&chat,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions WHERE sn = ?`, sn,
	)
	if err != nil {
		return nil, fmt.Errorf("%s (sn=%s): %w", i18n.T("db_session_not_found"), sn, err)
	}
	return &chat, nil
}

// ListDeletedChats lists the most recent N deleted chat records, ordered by update_at descending.
func (s *ChatStore) ListDeletedChats(n int) ([]Chat, error) {
	var chats []Chat
	err := s.db.Select(&chats,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		    extracted_at, extracted_count,
		    deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions
		 WHERE deleted = 1
		 ORDER BY update_at DESC
		 LIMIT ?`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_deleted_chats_failed"), err)
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
		return fmt.Errorf("%s: %w", i18n.T("db_restore_chat_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (sn=%s)", i18n.T("db_session_not_found"), sn)
	}
	return nil
}

// ListChats lists the most recent N non-deleted chat records, ordered by pinned first, then create_at descending.
func (s *ChatStore) ListChats(n int) ([]Chat, error) {
	var chats []Chat
	err := s.db.Select(&chats,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions
		 WHERE deleted = 0
		 ORDER BY pinned DESC, create_at DESC
		 LIMIT ?`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_chats_failed"), err)
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
		return fmt.Errorf("%s: %w", i18n.T("db_update_chat_title_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_session_not_found"), id)
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
		return fmt.Errorf("%s: %w", i18n.T("db_update_chat_pin_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_session_not_found"), id)
	}
	return nil
}

// UpdateChatTagged updates the tagged state of a chat.
func (s *ChatStore) UpdateChatTagged(id int64, taged bool) error {
	tagVal := 0
	if taged {
		tagVal = 1
	}
	result, err := s.db.Exec(
		"UPDATE chat_sessions SET taged = ? WHERE id = ?",
		tagVal, id,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_update_chat_tag_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_session_not_found"), id)
	}
	return nil
}

// EmptyTrash permanently deletes all soft-deleted chats (and their messages/web_sources).
func (s *ChatStore) EmptyTrash() error {
	// Get all deleted chat IDs first (related tables now use chat_id)
	var deletedIDs []int64
	err := s.db.Select(&deletedIDs,
		"SELECT id FROM chat_sessions WHERE deleted = 1",
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_query_deleted_chats_failed"), err)
	}

	if len(deletedIDs) == 0 {
		return nil
	}

	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_begin_transaction_failed"), err)
	}
	defer tx.Rollback()

	for _, id := range deletedIDs {
		// Delete all tags under this session
		_, err = tx.Exec("DELETE FROM chat_tags WHERE chat_id = ?", id)
		if err != nil {
			return fmt.Errorf("%s (id=%d): %w", i18n.T("db_delete_tags_for_chat_failed"), id, err)
		}

		// Delete all web sources under this session
		_, err = tx.Exec("DELETE FROM web_sources WHERE chat_id = ?", id)
		if err != nil {
			return fmt.Errorf("%s (id=%d): %w", i18n.T("db_delete_web_sources_for_chat_failed"), id, err)
		}

		// Delete all messages under this session
		_, err = tx.Exec("DELETE FROM chat_messages WHERE chat_id = ?", id)
		if err != nil {
			return fmt.Errorf("%s (id=%d): %w", i18n.T("db_delete_messages_for_chat_failed"), id, err)
		}
	}

	// Delete all deleted sessions
	_, err = tx.Exec("DELETE FROM chat_sessions WHERE deleted = 1")
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_trashed_sessions_failed"), err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_commit_transaction_failed"), err)
	}
	return nil
}

// ============================================================
// Extraction progress management
// ============================================================

// UpdateExtractionCountAndTime updates the trait extraction progress for a chat.
// Sets extracted_at to now() and adds to the extracted trait count.
// The increment parameter is the number of newly extracted traits in this round.
func (s *ChatStore) UpdateExtractionCountAndTime(chatID int64, increment int) error {
	result, err := s.db.Exec(
		`UPDATE chat_sessions
		 SET extracted_at = CURRENT_TIMESTAMP,
		     extracted_count = extracted_count + ?
		 WHERE id = ?`,
		increment, chatID,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_update_extraction_progress_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_session_not_found"), chatID)
	}
	return nil
}

// UpdateMessagesExtracted marks all messages in a chat up to (and including) the
// given message id as extracted (extracted = 1). This is called after a
// successful trait extraction to record which messages have been processed.
// Using message id (auto-increment PK) as the cutoff ensures that even if
// group_index is non-contiguous, all messages up to the given point are marked.
func (s *ChatStore) UpdateMessagesExtracted(chatID int64, upToID int64, extracted bool) error {
	extractedVal := 0
	if extracted {
		extractedVal = 1
	}
	_, err := s.db.Exec(
		`UPDATE chat_messages
		 SET extracted = ?
		 WHERE chat_id = ? AND id <= ? AND extracted = ?`,
		extractedVal, chatID, upToID, 1-extractedVal,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_mark_messages_extracted_failed"), err)
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
		return fmt.Errorf("%s: %w", i18n.T("db_touch_chat_update_at_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_session_not_found"), id)
	}
	return nil
}

type ChatTitle struct {
	ID      int64     `db:"id" json:"id"`
	Title   string    `db:"title" json:"title"`
	CrateAt time.Time `db:"create_at" json:"create_at"`
}

// ListChatTitles lists the titles of the most recent N non-deleted chat sessions, ordered by create_at descending.
func (s *ChatStore) ListChatTitles(n int) ([]ChatTitle, error) {
	var titles []ChatTitle
	err := s.db.Select(&titles,
		`SELECT id, title, create_at
		 FROM chat_sessions
		 WHERE deleted = 0
		 ORDER BY create_at DESC
		 LIMIT ?`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_chat_titles_failed"), err)
	}
	return titles, nil
}

type ChatTitleTag struct {
	SN    string `db:"sn" json:"sn"`
	Title string `db:"title" json:"title"`
	Tag   string `db:"tag" json:"tag"`

	CreateAt time.Time `db:"create_at" json:"create_at"`
	UpdateAt time.Time `db:"update_at" json:"update_at"`
}

// SelectChatTitlesGroupByTags 查询所有已分类对话，按 tag 分组，
// 组内先按 update_at 逆序，再按 create_at 逆序。
// 返回 map[string][]ChatTitleTag，key 为 tag 值。
func (s *ChatStore) SelectChatTitlesGroupByTags() (map[string][]ChatTitleTag, error) {
	var rows []ChatTitleTag
	err := s.db.Select(&rows,
		`SELECT cs.sn, cs.title, ct.tag, cs.create_at, cs.update_at
		 FROM chat_sessions cs
		 JOIN chat_tags ct ON cs.id = ct.chat_id
		 WHERE cs.deleted = 0
		 ORDER BY ct.tag, cs.update_at DESC, cs.create_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_select_chat_title_tag_groups_failed"), err)
	}

	result := make(map[string][]ChatTitleTag)
	for _, r := range rows {
		result[r.Tag] = append(result[r.Tag], r)
	}
	return result, nil
}
