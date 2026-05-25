package store

import (
	"fmt"
)

// InsertMessage 记录一条新消息
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

// DeleteMessageGroup 物理删除指定会话ID和组ID的消息
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
