package store

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// ChatStore provides access to chat-related tables in the global PostgreSQL database.
// It uses ThePGDB() internally, no per-user file management needed.
type ChatStore struct {
	// No per-user db field — uses ThePGDB() global singleton.
}

// NewChatStore creates a new ChatStore backed by the global PostgreSQL connection.
func NewChatStore() *ChatStore {
	return &ChatStore{}
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

// EnsureSchema ensures the chat database schema exists (idempotent).
// This runs CREATE TABLE IF NOT EXISTS and migration statements.
func (s *ChatStore) EnsureSchema() error {
	return s.initSchema()
}

// initSchema initializes chat-related table schemas.
func (s *ChatStore) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id             BIGSERIAL PRIMARY KEY,
			sn             VARCHAR(32)  NOT NULL UNIQUE,
			user_id        BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role_no        BIGINT       NOT NULL DEFAULT 0,
			title          TEXT         NOT NULL DEFAULT '',
			title_state    SMALLINT     NOT NULL DEFAULT 0,
			extract_mode   SMALLINT     NOT NULL DEFAULT 0,
			extracted_at   TIMESTAMPTZ,
			extracted_count INTEGER     NOT NULL DEFAULT 0,
			deleted        BOOLEAN      NOT NULL DEFAULT FALSE,
			pinned         BOOLEAN      NOT NULL DEFAULT FALSE,
			taged          BOOLEAN      NOT NULL DEFAULT FALSE,
			create_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			update_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_chat_sessions_user_id ON chat_sessions(user_id);
		CREATE INDEX IF NOT EXISTS idx_chat_sessions_sn      ON chat_sessions(sn);
		CREATE INDEX IF NOT EXISTS idx_chat_sessions_pinned  ON chat_sessions(pinned);

		CREATE TABLE IF NOT EXISTS chat_messages (
			id         BIGSERIAL PRIMARY KEY,
			chat_id    BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
			group_index INTEGER     NOT NULL,
			role       SMALLINT     NOT NULL,
			reasoning  TEXT,
			content    TEXT         NOT NULL,
			extracted  BOOLEAN      NOT NULL DEFAULT FALSE,
			interrupted SMALLINT    NOT NULL DEFAULT 0,
			create_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			update_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_id ON chat_messages(chat_id);

		CREATE TABLE IF NOT EXISTS web_sources (
			id           BIGSERIAL PRIMARY KEY,
			chat_id      BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
			msg_id       BIGINT       NOT NULL,
			title        TEXT         NOT NULL DEFAULT '',
			content      TEXT         NOT NULL DEFAULT '',
			url          TEXT         NOT NULL DEFAULT '',
			site_name    TEXT         NOT NULL DEFAULT '',
			site_icon    TEXT         NOT NULL DEFAULT '',
			publish_date TEXT         NOT NULL DEFAULT '',
			score        REAL         NOT NULL DEFAULT 0,
			create_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_web_sources_chat_msg ON web_sources(chat_id, msg_id);

		CREATE TABLE IF NOT EXISTS chat_tags (
			id        BIGSERIAL PRIMARY KEY,
			chat_id   BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
			tag       TEXT         NOT NULL,
			create_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_chat_tags_chat_id ON chat_tags(chat_id);
		CREATE INDEX IF NOT EXISTS idx_chat_tags_tag     ON chat_tags(tag);

		CREATE TABLE IF NOT EXISTS chat_favorites (
			id         BIGSERIAL PRIMARY KEY,
			chat_id    BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
			custom_tag TEXT         NOT NULL DEFAULT '',
			create_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			update_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);

		CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_favorites_unique ON chat_favorites(chat_id, custom_tag);
	`

	if _, err := ThePGDB().Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize chat tables: %w", err)
	}

	return nil
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
	var chat Chat
	err := s.db().Get(&chat,
		`INSERT INTO chat_sessions(sn, user_id, role_no, title, extract_mode)
		 VALUES($1, $2, $3, $4, $5)
		 RETURNING id, sn, role_no, title, title_state, extract_mode,
		           extracted_at, extracted_count,
		           deleted, pinned, taged, create_at, update_at`,
		sn, userID, roleNO, title, extractMode,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert chat: %w", err)
	}
	return &chat, nil
}

// LogicDelete soft-deletes the session identified by SN by setting deleted to true.
func (s *ChatStore) LogicDelete(sn string) error {
	result, err := s.db().Exec(
		"UPDATE chat_sessions SET deleted = TRUE WHERE sn = $1",
		sn,
	)
	if err != nil {
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
	result, err := s.db().Exec(
		"DELETE FROM chat_sessions WHERE id = $1",
		id,
	)
	if err != nil {
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
	var chat Chat
	err := s.db().Get(&chat,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions WHERE sn = $1`, sn,
	)
	if err != nil {
		return nil, fmt.Errorf("session not found (sn=%s): %w", sn, err)
	}
	return &chat, nil
}

// ListDeletedChats lists the most recent N deleted chat records for a user, ordered by update_at descending.
func (s *ChatStore) ListDeletedChats(userID int64, n int) ([]Chat, error) {
	var chats []Chat
	err := s.db().Select(&chats,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		    extracted_at, extracted_count,
		    deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions
		 WHERE user_id = $1 AND deleted = TRUE
		 ORDER BY update_at DESC
		 LIMIT $2`,
		userID, n,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list deleted chats: %w", err)
	}
	return chats, nil
}

// RestoreChat restores a soft-deleted chat by setting deleted = false.
func (s *ChatStore) RestoreChat(sn string) error {
	result, err := s.db().Exec(
		"UPDATE chat_sessions SET deleted = FALSE WHERE sn = $1",
		sn,
	)
	if err != nil {
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
	var chats []Chat
	err := s.db().Select(&chats,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions
		 WHERE user_id = $1 AND deleted = FALSE
		 ORDER BY pinned DESC, create_at DESC
		 LIMIT $2`,
		userID, n,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list chats: %w", err)
	}
	return chats, nil
}

// ListAllChats lists all non-deleted chats for a user (no limit).
func (s *ChatStore) ListAllChats(userID int64) ([]Chat, error) {
	var chats []Chat
	err := s.db().Select(&chats,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        extracted_at, extracted_count,
		        deleted, pinned, taged, create_at, update_at
		 FROM chat_sessions
		 WHERE user_id = $1 AND deleted = FALSE
		 ORDER BY pinned DESC, create_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list chats: %w", err)
	}
	return chats, nil
}

// UpdateChatTitle updates the chat title and title state.
func (s *ChatStore) UpdateChatTitle(id int64, title string, titleState int8) error {
	result, err := s.db().Exec(
		"UPDATE chat_sessions SET title = $1, title_state = $2 WHERE id = $3",
		title, titleState, id,
	)
	if err != nil {
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
	result, err := s.db().Exec(
		"UPDATE chat_sessions SET pinned = $1 WHERE id = $2",
		pinned, id,
	)
	if err != nil {
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
	result, err := s.db().Exec(
		"UPDATE chat_sessions SET taged = $1 WHERE id = $2",
		taged, id,
	)
	if err != nil {
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
	_, err := s.db().Exec(
		"DELETE FROM chat_sessions WHERE user_id = $1 AND deleted = TRUE",
		userID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete trashed sessions: %w", err)
	}
	return nil
}

// ============================================================
// Extraction progress management
// ============================================================

// UpdateExtractionCountAndTime updates the trait extraction progress for a chat.
func (s *ChatStore) UpdateExtractionCountAndTime(chatID int64, increment int) error {
	result, err := s.db().Exec(
		`UPDATE chat_sessions
		 SET extracted_at = NOW(),
		     extracted_count = extracted_count + $1
		 WHERE id = $2`,
		increment, chatID,
	)
	if err != nil {
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
	_, err := s.db().Exec(
		`UPDATE chat_messages
		 SET extracted = $1
		 WHERE chat_id = $2 AND id <= $3 AND extracted = $4`,
		extracted, chatID, upToID, !extracted,
	)
	if err != nil {
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
	result, err := s.db().Exec(
		"UPDATE chat_sessions SET update_at = NOW() WHERE id = $1",
		id,
	)
	if err != nil {
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
	var titles []ChatTitle
	err := s.db().Select(&titles,
		`SELECT id, title, create_at
		 FROM chat_sessions
		 WHERE user_id = $1 AND deleted = FALSE
		 ORDER BY create_at DESC
		 LIMIT $2`,
		userID, n,
	)
	if err != nil {
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
	var rows []ChatTitleTag
	err := s.db().Select(&rows,
		`SELECT cs.sn, cs.title, ct.tag, cs.create_at, cs.update_at
		 FROM chat_sessions cs
		 JOIN chat_tags ct ON cs.id = ct.chat_id
		 WHERE cs.user_id = $1 AND cs.deleted = FALSE
		 ORDER BY ct.tag, cs.update_at DESC, cs.create_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to select chat title tag groups: %w", err)
	}

	result := make(map[string][]ChatTitleTag)
	for _, r := range rows {
		result[r.Tag] = append(result[r.Tag], r)
	}
	return result, nil
}
