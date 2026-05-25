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
	用户对话记录
*/

type Session struct {
	ID int64  `db:"id" json:"id"` // 自增ID
	SN string `db:"sn" json:"sn"` // 全局唯一的SN串

	RoleNo int `db:"role_no" json:"role_no"` // 角色 / 人格编号

	Title      string `db:"title" json:"title"`             // 对话标题
	TitleState int8   `db:"title_state" json:"title_state"` // Title modification state 0: 原始(默认), 1: AI修改了，2： 用户修改了

	ExtractMode int8       `db:"extract_mode" json:"extract_mode"` // 0 手动，1 自动，默认0
	ExtractedAt *time.Time `db:"trait_time" json:"extracted_at"`   //  上次提取时间，默认null

	ExtractedMessageCount int  `db:"extracted_message_count" json:"extracted_message_count"` // 已提取消息数，默认0
	Deleted               bool `db:"deleted" json:"-"`                                       // 逻辑删除标记（JSON 中排除）
	Pinned                bool `db:"pinned" json:"pinned"`                                   // 是否置顶
	Category              int  `db:"category" json:"category"`                               // 所属分类，0=未分类

	CreateAt string `db:"create_at" json:"create_at"`
	UpdateAt string `db:"update_at" json:"update_at"`
}

type Message struct {
	ID        int64 `db:"id"`         // 自增ID
	SessionID int64 `db:"session_id"` // 所属对话会话ID

	GroupIndex int  `db:"group_index"` // 消息组序
	Role       int8 `db:"role"`        // 0: 用户 1: 助手

	Reasoning *string `db:"reasoning"`
	Content   string  `db:"content"` // 内容

	Extracted bool `db:"extracted"` // 是否已提取，默认为 0

	CreateAt string `db:"create_at"`
	UpdateAt string `db:"update_at"`
}

func CreateLocalChatScheme(dbFile string) (*ChatStore, error) {
	// 检查 dbFile (含路径，文件名) 指定的数据库是否存在
	// 如不存在，创建该库。包含两张表 chat_sessions 和 chat_messages
	// 分别对应上述两个结构体

	// 检查数据库文件是否存在
	_, err := os.Stat(dbFile)
	if os.IsNotExist(err) {
		// 文件不存在，创建空文件以确保 sqlx.Open 能正常工作
		f, err := os.Create(dbFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create chat database file. %w", err)
		}
		f.Close()
	}

	// 打开数据库（WAL 模式提升并发性能）
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

// initSchema 初始化聊天相关表结构
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
			extracted_message_count INTEGER NOT NULL DEFAULT 0,
			deleted   INTEGER NOT NULL DEFAULT 0,
			pinned    INTEGER NOT NULL DEFAULT 0,
			category  INTEGER NOT NULL DEFAULT 0,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS chat_messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL REFERENCES chat_sessions(id),
			group_index INTEGER NOT NULL,
			role       INTEGER NOT NULL,
			reasoning    TEXT,
			content    TEXT    NOT NULL,
			extracted  INTEGER NOT NULL DEFAULT 0,
			create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

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
	return nil
}

// InsertSession 创建一条新的会话，并返回
func (s *ChatStore) InsertSession(sn string, roleNO int,
	title string, extractMode int8) (*Session, error) {

	result, err := s.db.Exec(
		`INSERT INTO chat_sessions(sn, role_no, title, extract_mode)
		 VALUES(?, ?, ?, ?)`,
		sn, roleNO, title, extractMode,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert session. %w", err)
	}

	id, _ := result.LastInsertId()

	var sess Session
	err = s.db.Get(&sess,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        trait_time, extracted_message_count,
		        deleted, pinned, category, create_at, update_at
		 FROM chat_sessions WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query inserted session. %w", err)
	}
	return &sess, nil
}

// LogicDelete 逻辑删除SN指定的会话，仅需将记录 deleted 置为真
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

// PhysicalDelete 物理删除 id + sn 指定的会话
// 同时删除其下所有消息（事务安全）
func (s *ChatStore) PhysicalDelete(id int, sn string) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction. %w", err)
	}
	defer tx.Rollback()

	// 先查询会话是否存在，确保 id + sn 匹配
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

	// 删除该会话下的所有消息
	_, err = tx.Exec(
		"DELETE FROM chat_messages WHERE session_id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to delete messages of session. %w", err)
	}

	// 删除会话本身
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

// ListSessions 按置顶优先、update_at 从新到旧，列出最近N条未删除的会话记录
func (s *ChatStore) ListSessions(n int) ([]Session, error) {
	var sessions []Session
	err := s.db.Select(&sessions,
		`SELECT id, sn, role_no, title, title_state, extract_mode,
		        trait_time, extracted_message_count,
		        deleted, pinned, category, create_at, update_at
		 FROM chat_sessions
		 WHERE deleted = 0
		 ORDER BY pinned DESC, update_at DESC
		 LIMIT ?`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions. %w", err)
	}
	return sessions, nil
}

// UpdateSessionTitle 更新会话标题和标题状态
func (s *ChatStore) UpdateSessionTitle(id int64, title string, titleState int8) error {
	result, err := s.db.Exec(
		"UPDATE chat_sessions SET title = ?, title_state = ? WHERE id = ?",
		title, titleState, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update session title. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (id=%d)", id)
	}
	return nil
}

// UpdateSessionPin 更新会话置顶状态
func (s *ChatStore) UpdateSessionPin(id int64, pinned bool) error {
	pinVal := 0
	if pinned {
		pinVal = 1
	}
	result, err := s.db.Exec(
		"UPDATE chat_sessions SET pinned = ? WHERE id = ?",
		pinVal, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update session pin. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found (id=%d)", id)
	}
	return nil
}
