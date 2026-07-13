package store

import (
	"fmt"
	"time"

	"BrainForever/infra/zylog"

	"github.com/jmoiron/sqlx"
)

// ChatStore provides access to chat-related tables in the global PostgreSQL database.
// It uses ThePGDB() internally, no per-user file management needed.
type ChatStore struct {
	logger zylog.Logger
}

// NewChatStore creates a new ChatStore backed by the global PostgreSQL connection.
func NewChatStore(logger zylog.Logger) *ChatStore {
	return &ChatStore{
		logger: zylog.WrapWithSubject(logger, "store-chat"),
	}
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

// db returns the global PostgreSQL connection.
func (s *ChatStore) db() *sqlx.DB {
	return ThePGDB()
}

// ============================================================
// Chat CRUD
// ============================================================

// InsertChat creates a new chat session and returns it.
// userID is required for data isolation.
func (s *ChatStore) InsertChat(sn string, userID int64, roleNO int, title string, extractMode int8) (*Chat, error) {
	sqlStr := `INSERT INTO chat_sessions(sn, user_id, role_no, title, extract_mode)
		 VALUES($1, $2, $3, $4, $5)
		 RETURNING id, sn, role_no, title, title_state, extract_mode,
		           extracted_at, extracted_count,
		           deleted, pinned, taged, create_at, update_at`
	var chat Chat
	err := s.db().Get(&chat, sqlStr, sn, userID, roleNO, title, extractMode)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[sn=%s userID=%d]:\n%v", sqlStr, sn, userID, err)
		return nil, fmt.Errorf("failed to insert chat: %w", err)
	}
	return &chat, nil
}

// LogicDelete soft-deletes the session identified by SN by setting deleted to true.
func (s *ChatStore) LogicDelete(sn string) error {
	sqlStr := "UPDATE chat_sessions SET deleted = TRUE WHERE sn = $1"
	result, err := s.db().Exec(sqlStr, sn)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[sn=%s]:\n%v", sqlStr, sn, err)
		return fmt.Errorf("failed to delete session: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (sn=%s)", sn)
	}
	return nil
}

// PhysicalDelete physically deletes the session identified by id.
// Related rows (messages, web sources, tags, favorites) are automatically
// removed via ON DELETE CASCADE (PostgreSQL FK enforcement).
func (s *ChatStore) PhysicalDelete(id int) error {
	sqlStr := "DELETE FROM chat_sessions WHERE id = $1"
	result, err := s.db().Exec(sqlStr, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to delete session: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (id=%d)", id)
	}
	return nil
}

// FindChatBySN finds a chat by its SN (regardless of deleted status).
func (s *ChatStore) FindChatBySN(sn string) (*Chat, error) {
	sqlStr := `SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions WHERE sn = $1`
	var chat Chat
	err := s.db().Get(&chat, sqlStr, sn)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[sn=%s]:\n%v", sqlStr, sn, err)
		return nil, fmt.Errorf("session not found (sn=%s): %w", sn, err)
	}
	return &chat, nil
}

// ListDeletedChats lists the most recent N deleted chat records for a user, ordered by update_at descending.
func (s *ChatStore) ListDeletedChats(userID int64, n int) ([]Chat, error) {
	sqlStr := `SELECT id, sn, role_no, title, title_state, extract_mode,
		    extracted_at, extracted_count,
		    deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions
		 WHERE user_id = $1 AND deleted = TRUE
		 ORDER BY update_at DESC
		 LIMIT $2`
	var chats []Chat
	err := s.db().Select(&chats, sqlStr, userID, n)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to list deleted chats: %w", err)
	}
	return chats, nil
}

// RestoreChat restores a soft-deleted chat by setting deleted = false.
func (s *ChatStore) RestoreChat(sn string) error {
	sqlStr := "UPDATE chat_sessions SET deleted = FALSE WHERE sn = $1"
	result, err := s.db().Exec(sqlStr, sn)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[sn=%s]:\n%v", sqlStr, sn, err)
		return fmt.Errorf("failed to restore chat: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (sn=%s)", sn)
	}
	return nil
}

// ListChats lists the most recent N non-deleted chat records for a user, ordered by pinned first, then create_at descending.
func (s *ChatStore) ListChats(userID int64, n int) ([]Chat, error) {
	sqlStr := `SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions
		 WHERE user_id = $1 AND deleted = FALSE
		 ORDER BY pinned DESC, create_at DESC
		 LIMIT $2`
	var chats []Chat
	err := s.db().Select(&chats, sqlStr, userID, n)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to list chats: %w", err)
	}
	return chats, nil
}

// ListAllChats lists all non-deleted chats for a user (no limit).
func (s *ChatStore) ListAllChats(userID int64) ([]Chat, error) {
	sqlStr := `SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions
		 WHERE user_id = $1 AND deleted = FALSE
		 ORDER BY pinned DESC, create_at DESC`
	var chats []Chat
	err := s.db().Select(&chats, sqlStr, userID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to list chats: %w", err)
	}
	return chats, nil
}

// UpdateChatTitle updates the chat title and title state.
func (s *ChatStore) UpdateChatTitle(id int64, title string, titleState int8) error {
	sqlStr := "UPDATE chat_sessions SET title = $1, title_state = $2 WHERE id = $3"
	result, err := s.db().Exec(sqlStr, title, titleState, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to update chat title: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (id=%d)", id)
	}
	return nil
}

// UpdateChatPin updates the pinned state of a chat.
func (s *ChatStore) UpdateChatPin(id int64, pinned bool) error {
	sqlStr := "UPDATE chat_sessions SET pinned = $1 WHERE id = $2"
	result, err := s.db().Exec(sqlStr, pinned, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to update chat pin: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (id=%d)", id)
	}
	return nil
}

// UpdateChatTagged updates the tagged state of a chat.
func (s *ChatStore) UpdateChatTagged(id int64, taged bool) error {
	sqlStr := "UPDATE chat_sessions SET taged = $1 WHERE id = $2"
	result, err := s.db().Exec(sqlStr, taged, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to update chat tag: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (id=%d)", id)
	}
	return nil
}

// EmptyTrash permanently deletes all soft-deleted chats for a user.
func (s *ChatStore) EmptyTrash(userID int64) error {
	sqlStr := "DELETE FROM chat_sessions WHERE user_id = $1 AND deleted = TRUE"
	_, err := s.db().Exec(sqlStr, userID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return fmt.Errorf("failed to delete trashed sessions: %w", err)
	}
	return nil
}

// ============================================================
// Extraction progress management
// ============================================================

// UpdateExtractionCountAndTime updates the trait extraction progress for a chat.
func (s *ChatStore) UpdateExtractionCountAndTime(chatID int64, increment int) error {
	sqlStr := `UPDATE chat_sessions
		 SET extracted_at = NOW(),
		     extracted_count = extracted_count + $1
		 WHERE id = $2`
	result, err := s.db().Exec(sqlStr, increment, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d increment=%d]:\n%v", sqlStr, chatID, increment, err)
		return fmt.Errorf("failed to update extraction progress: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (id=%d)", chatID)
	}
	return nil
}

// UpdateMessagesExtracted marks all messages in a chat up to (and including) the
// given message id as extracted (extracted = true).
func (s *ChatStore) UpdateMessagesExtracted(chatID int64, upToID int64, extracted bool) error {
	sqlStr := `UPDATE chat_messages
		 SET extracted = $1
		 WHERE chat_id = $2 AND id <= $3 AND extracted = $4`
	_, err := s.db().Exec(sqlStr, extracted, chatID, upToID, !extracted)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d upToID=%d]:\n%v", sqlStr, chatID, upToID, err)
		return fmt.Errorf("failed to mark messages as extracted: %w", err)
	}
	return nil
}

// Close is a no-op because ChatStore no longer owns a connection.
func (s *ChatStore) Close() error {
	return nil
}

// TouchChat updates the update_at timestamp of a chat session to the current time.
func (s *ChatStore) TouchChat(id int64) error {
	sqlStr := "UPDATE chat_sessions SET update_at = NOW() WHERE id = $1"
	result, err := s.db().Exec(sqlStr, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to touch chat update_at: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (id=%d)", id)
	}
	return nil
}

type ChatTitle struct {
	ID      int64     `db:"id" json:"id"`
	Title   string    `db:"title" json:"title"`
	CrateAt time.Time `db:"create_at" json:"create_at"`
}

// ListChatTitles lists the titles of the most recent N non-deleted chat sessions for a user.
func (s *ChatStore) ListChatTitles(userID int64, n int) ([]ChatTitle, error) {
	sqlStr := `SELECT id, title, create_at
		 FROM chat_sessions
		 WHERE user_id = $1 AND deleted = FALSE
		 ORDER BY create_at DESC
		 LIMIT $2`
	var titles []ChatTitle
	err := s.db().Select(&titles, sqlStr, userID, n)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to list chat titles: %w", err)
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

// SelectChatTitlesGroupByTags queries all tagged chats for a user, grouped by tag.
func (s *ChatStore) SelectChatTitlesGroupByTags(userID int64) (map[string][]ChatTitleTag, error) {
	sqlStr := `SELECT cs.sn, cs.title, ct.tag, cs.create_at, cs.update_at
		 FROM chat_sessions cs
		 JOIN chat_tags ct ON cs.id = ct.chat_id
		 WHERE cs.user_id = $1 AND cs.deleted = FALSE
		 ORDER BY ct.tag, cs.update_at DESC, cs.create_at DESC`
	var rows []ChatTitleTag
	err := s.db().Select(&rows, sqlStr, userID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to select chat title tag groups: %w", err)
	}

	result := make(map[string][]ChatTitleTag)
	for _, r := range rows {
		result[r.Tag] = append(result[r.Tag], r)
	}
	return result, nil
}
