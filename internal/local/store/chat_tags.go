package store

import (
	"fmt"
)

// ============================================================
// ChatTags CRUD
// ============================================================

// SelectTagsGroup groups all tags by their value and returns a map of tag -> count.
func (s *ChatStore) SelectTagsGroup() (map[string]int, error) {
	var rows []struct {
		Tag   string `db:"tag"`
		Count int    `db:"cnt"`
	}

	err := s.db.Select(&rows,
		`SELECT tag, COUNT(1) AS cnt
		 FROM chat_tags
		 GROUP BY tag`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to select tag groups. %w", err)
	}

	result := make(map[string]int, len(rows))
	for _, r := range rows {
		result[r.Tag] = r.Count
	}
	return result, nil
}

// InsertChatTag creates a new chat tag and returns it.
func (s *ChatStore) InsertChatTag(chatID int64, tag string) (*ChatTag, error) {
	result, err := s.db.Exec(
		"INSERT INTO chat_tags(chat_id, tag) VALUES(?, ?)",
		chatID, tag,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert chat tag. %w", err)
	}

	id, _ := result.LastInsertId()

	var chatTag ChatTag
	err = s.db.Get(&chatTag,
		`SELECT id, chat_id, tag, create_at, update_at
		 FROM chat_tags WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query inserted chat tag. %w", err)
	}
	return &chatTag, nil
}

// ListChatTagsByChatID returns all tags for a given chat session,
// ordered by create_at ascending.
func (s *ChatStore) ListChatTagsByChatID(chatID int64) ([]ChatTag, error) {
	var tags []ChatTag
	err := s.db.Select(&tags,
		`SELECT id, chat_id, tag, create_at, update_at
		 FROM chat_tags
		 WHERE chat_id = ?
		 ORDER BY create_at ASC`,
		chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list chat tags. %w", err)
	}
	return tags, nil
}

// DeleteChatTag deletes a single chat tag by its ID.
func (s *ChatStore) DeleteChatTag(id int64) error {
	result, err := s.db.Exec(
		"DELETE FROM chat_tags WHERE id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to delete chat tag. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("chat tag not found (id=%d)", id)
	}
	return nil
}

// DeleteChatTagsByChatID deletes all tags for a given chat session.
func (s *ChatStore) DeleteChatTagsByChatID(chatID int64) error {
	_, err := s.db.Exec(
		"DELETE FROM chat_tags WHERE chat_id = ?",
		chatID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete chat tags for chat %d: %w", chatID, err)
	}
	return nil
}
