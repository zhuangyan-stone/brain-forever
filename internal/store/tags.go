package store

import (
	"fmt"
	"time"
)

// ============================================================
// ChatTags CRUD
// ============================================================

// ChatTag represents a tag associated with a chat session.
type ChatTag struct {
	ID       int64     `db:"id" json:"id"`
	ChatID   int64     `db:"chat_id" json:"chat_id"`
	Tag      string    `db:"tag" json:"tag"`
	CreateAt time.Time `db:"create_at" json:"create_at"`
}

// SelectTagsGroup groups all tags by their value and returns a map of tag -> count.
func (s *ChatStore) SelectTagsGroup() (map[string]int, error) {
	var rows []struct {
		Tag   string `db:"tag"`
		Count int    `db:"cnt"`
	}

	sqlStr := `SELECT tag, COUNT(1) AS cnt
		 FROM chat_tags
		 GROUP BY tag`
	err := s.db().Select(&rows, sqlStr)
	if err != nil {
		s.logger.Errorf("SQL [%s]:\n%v", sqlStr, err)
		return nil, fmt.Errorf("failed to select tag groups. %w", err)
	}

	result := make(map[string]int, len(rows))
	for _, r := range rows {
		result[r.Tag] = r.Count
	}
	return result, nil
}

// SelectNonEmptyTagsGroup is like SelectTagsGroup but filters out empty-string tags.
func (s *ChatStore) SelectNonEmptyTagsGroup() (map[string]int, error) {
	var rows []struct {
		Tag   string `db:"tag"`
		Count int    `db:"cnt"`
	}

	sqlStr := `SELECT tag, COUNT(1) AS cnt
		 FROM chat_tags
		 WHERE tag != ''
		 GROUP BY tag`
	err := s.db().Select(&rows, sqlStr)
	if err != nil {
		s.logger.Errorf("SQL [%s]:\n%v", sqlStr, err)
		return nil, fmt.Errorf("failed to select non-empty tag groups. %w", err)
	}

	result := make(map[string]int, len(rows))
	for _, r := range rows {
		result[r.Tag] = r.Count
	}
	return result, nil
}

// InsertChatTag creates a new chat tag and returns it.
func (s *ChatStore) InsertChatTag(chatID int64, tag string) (*ChatTag, error) {
	sqlStr := `INSERT INTO chat_tags(chat_id, tag)
		 VALUES($1, $2)
		 RETURNING id, chat_id, tag, create_at`
	var chatTag ChatTag
	err := s.db().Get(&chatTag, sqlStr, chatID, tag)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return nil, fmt.Errorf("failed to insert chat tag. %w", err)
	}
	return &chatTag, nil
}

// ListChatTagsByChatID returns all tags for a given chat session.
func (s *ChatStore) ListChatTagsByChatID(chatID int64) ([]ChatTag, error) {
	sqlStr := `SELECT id, chat_id, tag, create_at
		 FROM chat_tags
		 WHERE chat_id = $1
		 ORDER BY create_at ASC`
	var tags []ChatTag
	err := s.db().Select(&tags, sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return nil, fmt.Errorf("failed to list chat tags. %w", err)
	}
	return tags, nil
}

// DeleteChatTag deletes a single chat tag by its ID.
func (s *ChatStore) DeleteChatTag(id int64) error {
	sqlStr := "DELETE FROM chat_tags WHERE id = $1"
	result, err := s.db().Exec(sqlStr, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
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
	sqlStr := "DELETE FROM chat_tags WHERE chat_id = $1"
	_, err := s.db().Exec(sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return fmt.Errorf("failed to delete chat tags for chat (id=%d). %w", chatID, err)
	}
	return nil
}
