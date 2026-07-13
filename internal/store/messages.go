package store

import (
	"fmt"
	"time"
)

type Message struct {
	ID     int64 `db:"id" json:"id"`           // Auto-increment ID
	ChatID int64 `db:"chat_id" json:"chat_id"` // Belonging chat ID (chat_sessions.id)

	GroupIndex int  `db:"group_index" json:"group_index"` // Message group index
	Role       int8 `db:"role" json:"role"`               // 0: user 1: assistant

	Reasoning *string `db:"reasoning"`
	Content   string  `db:"content"` // Content

	Extracted   bool `db:"extracted" json:"extracted"`     // Whether extracted
	Interrupted int  `db:"interrupted" json:"interrupted"` // 0=done, 1=user-interrupted, 2=backend-error

	CreateAt time.Time `db:"create_at"`
	UpdateAt time.Time `db:"update_at"`
}

// InsertMessage records a new message.
func (s *ChatStore) InsertMessage(chatID int64, groupIndex int, role int,
	content string, reasoning *string, interrupted int) error {
	sqlStr := `INSERT INTO chat_messages(chat_id, group_index, role, reasoning, content, interrupted)
		 VALUES($1, $2, $3, $4, $5, $6)`
	_, err := s.db().Exec(sqlStr, chatID, groupIndex, role, reasoning, content, interrupted)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d groupIndex=%d]: %v", sqlStr, chatID, groupIndex, err)
		return fmt.Errorf("failed to insert message: %w", err)
	}
	return nil
}

// ListMessages queries all messages of a given chat, sorted by group_index and id.
func (s *ChatStore) ListMessages(chatID int64) ([]Message, error) {
	sqlStr := `SELECT id, chat_id, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_id = $1
		 ORDER BY group_index ASC, id ASC`
	var msgs []Message
	err := s.db().Select(&msgs, sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]: %v", sqlStr, chatID, err)
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}
	return msgs, nil
}

// ListMessagesByRange queries messages of a given chat starting from a specific message ID.
func (s *ChatStore) ListMessagesByRange(chatID int64, startID int64, limit int) ([]Message, error) {
	sqlStr := `SELECT id, chat_id, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_id = $1 AND id > $2
		 ORDER BY id ASC
		 LIMIT $3`
	var msgs []Message
	err := s.db().Select(&msgs, sqlStr, chatID, startID, limit)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d startID=%d]: %v", sqlStr, chatID, startID, err)
		return nil, fmt.Errorf("failed to list messages by range: %w", err)
	}
	return msgs, nil
}

// ListUnExtractMessages queries only un-extracted messages of a given chat.
func (s *ChatStore) ListUnExtractMessages(chatID int64) ([]Message, error) {
	sqlStr := `SELECT id, chat_id, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_id = $1 AND extracted = FALSE
		 ORDER BY group_index ASC, id ASC`
	var msgs []Message
	err := s.db().Select(&msgs, sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]: %v", sqlStr, chatID, err)
		return nil, fmt.Errorf("failed to list unextracted messages: %w", err)
	}
	return msgs, nil
}

// CountMessages returns the total number of messages in a chat.
func (s *ChatStore) CountMessages(chatID int64) (int, error) {
	sqlStr := "SELECT COUNT(1) FROM chat_messages WHERE chat_id = $1"
	var count int
	err := s.db().Get(&count, sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]: %v", sqlStr, chatID, err)
		return 0, fmt.Errorf("failed to count messages: %w", err)
	}
	return count, nil
}

// DeleteMessageGroup physically deletes messages and their associated web sources
// for the given chat ID and group index (transaction-safe).
func (s *ChatStore) DeleteMessageGroup(chatID int64, groupIndex int) error {
	tx, err := s.db().Beginx()
	if err != nil {
		s.logger.Errorf("BEGIN transaction failed: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete web sources first
	sqlDelWeb := "DELETE FROM web_sources WHERE chat_id = $1 AND msg_id = $2"
	_, err = tx.Exec(sqlDelWeb, chatID, groupIndex)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d groupIndex=%d]: %v", sqlDelWeb, chatID, groupIndex, err)
		return fmt.Errorf("failed to delete web sources for message group: %w", err)
	}

	// Delete messages
	sqlDelMsg := "DELETE FROM chat_messages WHERE chat_id = $1 AND group_index = $2"
	_, err = tx.Exec(sqlDelMsg, chatID, groupIndex)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d groupIndex=%d]: %v", sqlDelMsg, chatID, groupIndex, err)
		return fmt.Errorf("failed to delete message group: %w", err)
	}

	return tx.Commit()
}

// FindMessageByID finds a message by its ID.
func (s *ChatStore) FindMessageByID(msgID int64) (*Message, error) {
	sqlStr := `SELECT id, chat_id, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages WHERE id = $1`
	var msg Message
	err := s.db().Get(&msg, sqlStr, msgID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[msgID=%d]: %v", sqlStr, msgID, err)
		return nil, fmt.Errorf("message not found (id=%d): %w", msgID, err)
	}
	return &msg, nil
}
