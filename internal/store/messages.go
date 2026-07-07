package store

import (
	"fmt"
	"time"
)

type Message struct {
	ID     int64 `db:"id"`      // Auto-increment ID
	ChatID int64 `db:"chat_id"` // Belonging chat ID (chat_sessions.id)

	GroupIndex int  `db:"group_index"` // Message group index
	Role       int8 `db:"role"`        // 0: user 1: assistant

	Reasoning *string `db:"reasoning"`
	Content   string  `db:"content"` // Content

	Extracted   bool `db:"extracted"`   // Whether extracted, default 0
	Interrupted int  `db:"interrupted"` // 0=done, 1=user-interrupted, 2=backend-error

	CreateAt time.Time `db:"create_at"`
	UpdateAt time.Time `db:"update_at"`
}

// InsertMessage records a new message.
func (s *ChatStore) InsertMessage(chatID int64, groupIndex int, role int,
	content string, reasoning *string, interrupted int) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_messages(chat_id, group_index, role, reasoning, content, interrupted)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		chatID, groupIndex, role, reasoning, content, interrupted,
	)
	if err != nil {
		return fmt.Errorf("failed to insert message: %w", err)
	}
	return nil
}

// ListMessages queries all messages of a given chat, sorted by group_index and id.
func (s *ChatStore) ListMessages(chatID int64) ([]Message, error) {
	var msgs []Message
	err := s.db.Select(&msgs,
		`SELECT id, chat_id, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_id = ?
		 ORDER BY group_index ASC, id ASC`,
		chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}
	return msgs, nil
}

// ListMessagesByRange queries messages of a given chat starting from a specific message ID,
// limited to a specific count, ordered by id ASC.
// If startID is 0, starts from the first message (id > 0).
func (s *ChatStore) ListMessagesByRange(chatID int64, startID int64, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.Select(&msgs,
		`SELECT id, chat_id, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_id = ? AND id > ?
		 ORDER BY id ASC
		 LIMIT ?`,
		chatID, startID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages by range: %w", err)
	}
	return msgs, nil
}

// ListUnExtractMessages queries only un-extracted messages (extracted = 0) of a given chat,
// sorted by group_index and id. This is used by the trait extraction handler to avoid
// fetching already-extracted messages from the database layer.
func (s *ChatStore) ListUnExtractMessages(chatID int64) ([]Message, error) {
	var msgs []Message
	err := s.db.Select(&msgs,
		`SELECT id, chat_id, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_id = ? AND extracted = 0
		 ORDER BY group_index ASC, id ASC`,
		chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list unextracted messages: %w", err)
	}
	return msgs, nil
}

// CountMessages returns the total number of messages in a chat.
func (s *ChatStore) CountMessages(chatID int64) (int, error) {
	var count int
	err := s.db.Get(&count,
		`SELECT COUNT(1) FROM chat_messages WHERE chat_id = ?`,
		chatID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to count messages: %w", err)
	}
	return count, nil
}

// DeleteMessageGroup physically deletes messages and their associated web sources
// for the given chat ID and group index (transaction-safe).
func (s *ChatStore) DeleteMessageGroup(chatID int64, groupIndex int) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete web sources first (foreign key or not, clean up orphans)
	_, err = tx.Exec(
		"DELETE FROM web_sources WHERE chat_id = ? AND msg_id = ?",
		chatID, groupIndex,
	)
	if err != nil {
		return fmt.Errorf("failed to delete web sources for message group: %w", err)
	}

	// Delete messages
	_, err = tx.Exec(
		"DELETE FROM chat_messages WHERE chat_id = ? AND group_index = ?",
		chatID, groupIndex,
	)
	if err != nil {
		return fmt.Errorf("failed to delete message group: %w", err)
	}

	return tx.Commit()
}
