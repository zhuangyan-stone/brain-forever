package store

import (
	"fmt"
	"time"

	"BrainForever/infra/i18n"
)

type Message struct {
	ID     int64  `db:"id"`      // Auto-increment ID
	ChatSN string `db:"chat_sn"` // Belonging chat SN (chat_sessions.sn)

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
func (s *ChatStore) InsertMessage(chatSN string, groupIndex int, role int,
	content string, reasoning *string, interrupted int) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_messages(chat_sn, group_index, role, reasoning, content, interrupted)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		chatSN, groupIndex, role, reasoning, content, interrupted,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_insert_message_failed"), err)
	}
	return nil
}

// ListMessages queries all messages of a given chat, sorted by group_index and id.
func (s *ChatStore) ListMessages(chatSN string) ([]Message, error) {
	var msgs []Message
	err := s.db.Select(&msgs,
		`SELECT id, chat_sn, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_sn = ?
		 ORDER BY group_index ASC, id ASC`,
		chatSN,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_messages_failed"), err)
	}
	return msgs, nil
}

// ListMessagesByRange queries messages of a given chat starting from a specific message ID,
// limited to a specific count, ordered by id ASC.
// If startID is 0, starts from the first message (id > 0).
func (s *ChatStore) ListMessagesByRange(chatSN string, startID int64, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.Select(&msgs,
		`SELECT id, chat_sn, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_sn = ? AND id > ?
		 ORDER BY id ASC
		 LIMIT ?`,
		chatSN, startID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_messages_by_range_failed"), err)
	}
	return msgs, nil
}

// ListUnExtractMessages queries only un-extracted messages (extracted = 0) of a given chat,
// sorted by group_index and id. This is used by the trait extraction handler to avoid
// fetching already-extracted messages from the database layer.
func (s *ChatStore) ListUnExtractMessages(chatSN string) ([]Message, error) {
	var msgs []Message
	err := s.db.Select(&msgs,
		`SELECT id, chat_sn, group_index, role, reasoning, content,
		        extracted, interrupted, create_at, update_at
		 FROM chat_messages
		 WHERE chat_sn = ? AND extracted = 0
		 ORDER BY group_index ASC, id ASC`,
		chatSN,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_unextracted_messages_failed"), err)
	}
	return msgs, nil
}

// CountMessages returns the total number of messages in a chat.
func (s *ChatStore) CountMessages(chatSN string) (int, error) {
	var count int
	err := s.db.Get(&count,
		`SELECT COUNT(1) FROM chat_messages WHERE chat_sn = ?`,
		chatSN,
	)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", i18n.T("db_count_messages_failed"), err)
	}
	return count, nil
}

// DeleteMessageGroup physically deletes messages and their associated web sources
// for the given chat SN and group index (transaction-safe).
func (s *ChatStore) DeleteMessageGroup(chatSN string, groupIndex int) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_begin_transaction_failed"), err)
	}
	defer tx.Rollback()

	// Delete web sources first (foreign key or not, clean up orphans)
	_, err = tx.Exec(
		"DELETE FROM web_sources WHERE chat_sn = ? AND msg_id = ?",
		chatSN, groupIndex,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_web_sources_for_message_group_failed"), err)
	}

	// Delete messages
	_, err = tx.Exec(
		"DELETE FROM chat_messages WHERE chat_sn = ? AND group_index = ?",
		chatSN, groupIndex,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_message_group_failed"), err)
	}

	return tx.Commit()
}
