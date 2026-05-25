package store

import (
	"fmt"
)

// InsertMessage records a new message.
func (s *ChatStore) InsertMessage(sessionID int64, groupIndex int, role int,
	content string, reasoning *string) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_messages(session_id, group_index, role, reasoning, content)
		 VALUES(?, ?, ?, ?, ?)`,
		sessionID, groupIndex, role, reasoning, content,
	)
	if err != nil {
		return fmt.Errorf("failed to insert message. %w", err)
	}
	return nil
}

// ListMessages queries all messages of a given session, sorted by group_index and id.
func (s *ChatStore) ListMessages(sessionID int64) ([]Message, error) {
	var msgs []Message
	err := s.db.Select(&msgs,
		`SELECT id, session_id, group_index, role, reasoning, content,
		        extracted, create_at, update_at
		 FROM chat_messages
		 WHERE session_id = ?
		 ORDER BY group_index ASC, id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}
	return msgs, nil
}

// DeleteMessageGroup physically deletes messages matching the given session ID and group index.
func (s *ChatStore) DeleteMessageGroup(sessionID int64, groupIndex int) error {
	_, err := s.db.Exec(
		"DELETE FROM chat_messages WHERE session_id = ? AND group_index = ?",
		sessionID, groupIndex,
	)

	if err != nil {
		return fmt.Errorf("failed to delete message group. %w", err)
	}

	return nil
}
