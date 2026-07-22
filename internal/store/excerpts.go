package store

import (
	"database/sql"
	"fmt"
	"time"

	"BrainForever/infra/zylog"

	"github.com/jmoiron/sqlx"
)

// ============================================================
// ExcerptValueDict - excerpt value type dictionary
// ============================================================

// ExcerptValueDict represents a row in the excerpt_value_dict table.
type ExcerptValueDict struct {
	ID      int16  `db:"id"`
	Value   string `db:"value"`
	ValueCN string `db:"value_cn"`
}

// ============================================================
// Excerpt - user quote excerpt entity
// ============================================================

// Excerpt represents a row in the excerpts table.
type Excerpt struct {
	ID             int64      `db:"id"`
	UserID         int64      `db:"user_id"`
	ChatID         int64      `db:"chat_id"`
	MsgID          int64      `db:"msg_id"`
	MsgTime        time.Time  `db:"msg_time"`    // source message creation time (non-null)
	LastRefAt      *time.Time `db:"last_ref_at"` // last referenced time (null when never referenced)
	Values         []int16    `db:"values"`
	Content        string     `db:"content"`
	ContextSummary string     `db:"context_summary"`
	Reason         string     `db:"reason"`
	CreateAt       time.Time  `db:"create_at"`
}

// ============================================================
// ExcerptStore
// ============================================================

// ExcerptStore provides access to the excerpts and excerpt_value_dict
// tables in the global PostgreSQL database.
type ExcerptStore struct {
	logger zylog.Logger
}

// NewExcerptStore creates a new ExcerptStore.
func NewExcerptStore(logger zylog.Logger) *ExcerptStore {
	return &ExcerptStore{
		logger: zylog.WrapWithSubject(logger, "store-excerpt"),
	}
}

// db returns the global PostgreSQL connection.
func (s *ExcerptStore) db() *sqlx.DB {
	return ThePGDB()
}

// ============================================================
// ExcerptValueDict CRUD
// ============================================================

// ListAllValueDicts returns all rows from excerpt_value_dict.
func (s *ExcerptStore) ListAllValueDicts() ([]ExcerptValueDict, error) {
	sqlStr := `SELECT id, value, value_cn
		 FROM excerpt_value_dict
		 ORDER BY id`
	var rows []ExcerptValueDict
	err := s.db().Select(&rows, sqlStr)
	if err != nil {
		s.logger.Errorf("SQL [%s]:\n%v", sqlStr, err)
		return nil, fmt.Errorf("failed to list excerpt value dict. %w", err)
	}
	return rows, nil
}

// GetValueDictByID returns a single excerpt_value_dict row by ID.
// Returns nil and no error if not found.
func (s *ExcerptStore) GetValueDictByID(id int16) (*ExcerptValueDict, error) {
	sqlStr := `SELECT id, value, value_cn
		 FROM excerpt_value_dict
		 WHERE id = $1`
	var row ExcerptValueDict
	err := s.db().Get(&row, sqlStr, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return nil, fmt.Errorf("failed to get value dict by id. %w", err)
	}
	return &row, nil
}

// GetValueDictByValue returns a single excerpt_value_dict row by value string.
// Returns nil and no error if not found.
func (s *ExcerptStore) GetValueDictByValue(value string) (*ExcerptValueDict, error) {
	sqlStr := `SELECT id, value, value_cn
		 FROM excerpt_value_dict
		 WHERE value = $1`
	var row ExcerptValueDict
	err := s.db().Get(&row, sqlStr, value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		s.logger.Errorf("SQL [%s] args=[value=%s]:\n%v", sqlStr, value, err)
		return nil, fmt.Errorf("failed to get value dict by value. %w", err)
	}
	return &row, nil
}

// ============================================================
// Excerpt CRUD
// ============================================================

// ExcerptInsertion holds all data needed to insert a single excerpt.
type ExcerptInsertion struct {
	UserID         int64
	ChatID         int64
	MsgID          int64
	MsgTime        time.Time // source message creation time
	Values         []int16
	Content        string
	ContextSummary string
	Reason         string
}

// InsertExcerpt inserts a new excerpt record.
func (s *ExcerptStore) InsertExcerpt(in *ExcerptInsertion) (*Excerpt, error) {
	sqlStr := `INSERT INTO excerpts(user_id, chat_id, msg_id, msg_time, values, content, context_summary, reason)
		 VALUES($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, create_at`
	var excerpt Excerpt
	err := s.db().QueryRow(sqlStr,
		in.UserID, in.ChatID, in.MsgID, in.MsgTime, in.Values,
		in.Content, in.ContextSummary, in.Reason,
	).Scan(&excerpt.ID, &excerpt.CreateAt)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d chatID=%d msgID=%d]:\n%v", sqlStr, in.UserID, in.ChatID, in.MsgID, err)
		return nil, fmt.Errorf("failed to insert excerpt. %w", err)
	}
	excerpt.UserID = in.UserID
	excerpt.ChatID = in.ChatID
	excerpt.MsgID = in.MsgID
	excerpt.MsgTime = in.MsgTime
	excerpt.Values = in.Values
	excerpt.Content = in.Content
	excerpt.ContextSummary = in.ContextSummary
	excerpt.Reason = in.Reason
	return &excerpt, nil
}

// BatchInsertExcerpts inserts multiple excerpts in a single transaction.
func (s *ExcerptStore) BatchInsertExcerpts(insertions []ExcerptInsertion) (int, error) {
	if len(insertions) == 0 {
		return 0, nil
	}

	tx, err := s.db().Beginx()
	if err != nil {
		return 0, fmt.Errorf("BEGIN transaction failed. %w", err)
	}
	defer tx.Rollback()

	sqlStr := `INSERT INTO excerpts(user_id, chat_id, msg_id, msg_time, values, content, context_summary, reason)
		 VALUES($1, $2, $3, $4, $5, $6, $7, $8)`

	for _, in := range insertions {
		_, err := tx.Exec(sqlStr,
			in.UserID, in.ChatID, in.MsgID, in.MsgTime, in.Values,
			in.Content, in.ContextSummary, in.Reason,
		)
		if err != nil {
			s.logger.Errorf("SQL [%s] args=[userID=%d chatID=%d msgID=%d]:\n%v",
				sqlStr, in.UserID, in.ChatID, in.MsgID, err)
			return 0, fmt.Errorf("failed to batch insert excerpt. %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("COMMIT transaction failed. %w", err)
	}
	return len(insertions), nil
}

// GetExcerptByID returns a single excerpt by its ID.
func (s *ExcerptStore) GetExcerptByID(id int64) (*Excerpt, error) {
	sqlStr := `SELECT id, user_id, chat_id, msg_id, msg_time, last_ref_at, values, content,
		          context_summary, reason, create_at
		 FROM excerpts
		 WHERE id = $1`
	var excerpt Excerpt
	err := s.db().Get(&excerpt, sqlStr, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return nil, fmt.Errorf("failed to get excerpt by id. %w", err)
	}
	return &excerpt, nil
}

// ListExcerptsByUser returns excerpts for a user, ordered by create_at descending.
func (s *ExcerptStore) ListExcerptsByUser(userID int64, limit int, offset int) ([]Excerpt, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	sqlStr := `SELECT id, user_id, chat_id, msg_id, msg_time, last_ref_at, values, content,
		          context_summary, reason, create_at
		 FROM excerpts
		 WHERE user_id = $1
		 ORDER BY create_at DESC
		 LIMIT $2 OFFSET $3`
	var rows []Excerpt
	err := s.db().Select(&rows, sqlStr, userID, limit, offset)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d limit=%d offset=%d]:\n%v", sqlStr, userID, limit, offset, err)
		return nil, fmt.Errorf("failed to list excerpts by user. %w", err)
	}
	return rows, nil
}

// ListExcerptsByChat returns excerpts for a specific chat, ordered by create_at descending.
func (s *ExcerptStore) ListExcerptsByChat(chatID int64, limit int) ([]Excerpt, error) {
	if limit <= 0 {
		limit = 100
	}

	sqlStr := `SELECT id, user_id, chat_id, msg_id, msg_time, last_ref_at, values, content,
		          context_summary, reason, create_at
		 FROM excerpts
		 WHERE chat_id = $1
		 ORDER BY create_at DESC
		 LIMIT $2`
	var rows []Excerpt
	err := s.db().Select(&rows, sqlStr, chatID, limit)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d limit=%d]:\n%v", sqlStr, chatID, limit, err)
		return nil, fmt.Errorf("failed to list excerpts by chat. %w", err)
	}
	return rows, nil
}

// ListExcerptsByValue returns excerpts for a user filtered by a specific value type,
// ordered by create_at descending.
func (s *ExcerptStore) ListExcerptsByValue(userID int64, valueID int16, limit int, offset int) ([]Excerpt, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	sqlStr := `SELECT id, user_id, chat_id, msg_id, msg_time, last_ref_at, values, content,
		          context_summary, reason, create_at
		 FROM excerpts
		 WHERE user_id = $1 AND $2 = ANY(values)
		 ORDER BY create_at DESC
		 LIMIT $3 OFFSET $4`
	var rows []Excerpt
	err := s.db().Select(&rows, sqlStr, userID, valueID, limit, offset)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d valueID=%d limit=%d offset=%d]:\n%v",
			sqlStr, userID, valueID, limit, offset, err)
		return nil, fmt.Errorf("failed to list excerpts by value. %w", err)
	}
	return rows, nil
}

// UpdateLastRefAt sets last_ref_at = NOW() for the given excerpt IDs.
// Useful when excerpts are referenced in conversation context.
func (s *ExcerptStore) UpdateLastRefAt(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	sqlStr := `UPDATE excerpts SET last_ref_at = NOW() WHERE id = ANY($1)`
	_, err := s.db().Exec(sqlStr, ids)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[ids=%v]:\n%v", sqlStr, ids, err)
		return fmt.Errorf("failed to update last_ref_at. %w", err)
	}
	return nil
}

// DeleteExcerpt deletes an excerpt by ID.
func (s *ExcerptStore) DeleteExcerpt(id int64) error {
	sqlStr := "DELETE FROM excerpts WHERE id = $1"
	result, err := s.db().Exec(sqlStr, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to delete excerpt. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("excerpt not found (id=%d)", id)
	}
	return nil
}

// DeleteExcerptsByChat deletes all excerpts for a given chat.
func (s *ExcerptStore) DeleteExcerptsByChat(chatID int64) (int, error) {
	sqlStr := "DELETE FROM excerpts WHERE chat_id = $1"
	result, err := s.db().Exec(sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return 0, fmt.Errorf("failed to delete excerpts by chat. %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// CountExcerptsByUser returns the total number of excerpts for a user.
func (s *ExcerptStore) CountExcerptsByUser(userID int64) (int, error) {
	sqlStr := "SELECT COUNT(*) FROM excerpts WHERE user_id = $1"
	var count int
	err := s.db().Get(&count, sqlStr, userID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return 0, fmt.Errorf("failed to count excerpts. %w", err)
	}
	return count, nil
}

// ============================================================
// ChatPendingExcerpt — pending chat for excerpt extraction
// ============================================================

// ChatPendingExcerpt represents a chat session that may need excerpt extraction.
// Includes user_id and settings so the caller can look up per-user API/lang settings.
type ChatPendingExcerpt struct {
	ID          int64      `db:"id"`
	UserID      int64      `db:"user_id"`
	Title       string     `db:"title"`
	ProcessedAt *time.Time `db:"processed_at"` // excerpt_progress.processed_at
	UpdateAt    time.Time  `db:"update_at"`
	Settings    string     `db:"settings"` // JSONB from users.settings
}

// ListChatsPendingExcerpt queries chat sessions eligible for excerpt extraction.
//
// Criteria: deleted=false AND (processed_at IS NULL OR processed_at < update_at - delayHours).
// Results are ordered by update_at ascending so older/changed chats are processed first.
// batchLimit caps the number of results to prevent overloading the LLM API.
func (s *ExcerptStore) ListChatsPendingExcerpt(delayHours int, batchLimit int) ([]ChatPendingExcerpt, error) {
	sqlStr := `SELECT cs.id, cs.user_id, cs.title, cs.update_at,
		          cep.processed_at,
		          u.settings
	           FROM chat_sessions cs
	           JOIN users u ON u.id = cs.user_id
	           LEFT JOIN excerpt_progress cep ON cep.chat_id = cs.id
	           WHERE cs.deleted = FALSE
	             AND (cep.processed_at IS NULL
	               OR cep.processed_at < cs.update_at - ($1::text || ' hours')::interval)
	           ORDER BY cs.update_at ASC
	           LIMIT $2`
	var rows []ChatPendingExcerpt
	err := s.db().Select(&rows, sqlStr, fmt.Sprintf("%d", delayHours), batchLimit)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[delayHours=%d batchLimit=%d]:\n%v", sqlStr, delayHours, batchLimit, err)
		return nil, fmt.Errorf("failed to list chats pending excerpt. %w", err)
	}
	return rows, nil
}

// UpsertExcerptProgress inserts or updates the excerpt processing progress
// for a chat session. Sets processed_at to the current time.
func (s *ExcerptStore) UpsertExcerptProgress(chatID int64) error {
	sqlStr := `INSERT INTO excerpt_progress(chat_id, processed_at)
	           VALUES($1, NOW())
	           ON CONFLICT (chat_id) DO UPDATE SET processed_at = NOW()`
	_, err := s.db().Exec(sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return fmt.Errorf("failed to upsert excerpt progress. %w", err)
	}
	return nil
}

// Close is a no-op because ExcerptStore does not own a connection.
func (s *ExcerptStore) Close() error {
	return nil
}
