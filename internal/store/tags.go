package store

import (
	"fmt"
	"time"

	"BrainForever/infra/i18n"
)

// ============================================================
// ChatTags CRUD
// ============================================================

// ChatTag represents a tag associated with a chat session.
type ChatTag struct {
	ID       int64     `db:"id" json:"id"`               // Auto-increment primary key
	ChatID   int64     `db:"chat_id" json:"chat_id"`     // References chat_sessions.id
	Tag      string    `db:"tag" json:"tag"`             // Tag string (topic classification)
	CreateAt time.Time `db:"create_at" json:"create_at"` // Creation time
}

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
		return nil, fmt.Errorf("%s: %w", i18n.T("db_select_tag_groups_failed"), err)
	}

	result := make(map[string]int, len(rows))
	for _, r := range rows {
		result[r.Tag] = r.Count
	}
	return result, nil
}

// SelectNonEmptyTagsGroup is like SelectTagsGroup but filters out empty-string tags.
// This is used when building the LLM prompt, so the empty placeholder tag
// (saved when LLM returns no tags) doesn't pollute the tag usage statistics.
func (s *ChatStore) SelectNonEmptyTagsGroup() (map[string]int, error) {
	var rows []struct {
		Tag   string `db:"tag"`
		Count int    `db:"cnt"`
	}

	err := s.db.Select(&rows,
		`SELECT tag, COUNT(1) AS cnt
		 FROM chat_tags
		 WHERE tag != ''
		 GROUP BY tag`,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_select_non_empty_tag_groups_failed"), err)
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
		return nil, fmt.Errorf("%s: %w", i18n.T("db_insert_chat_tag_failed"), err)
	}

	id, _ := result.LastInsertId()

	var chatTag ChatTag
	err = s.db.Get(&chatTag,
		`SELECT id, chat_id, tag, create_at
		 FROM chat_tags WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_query_inserted_chat_tag_failed"), err)
	}
	return &chatTag, nil
}

// ListChatTagsByChatID returns all tags for a given chat session,
// ordered by create_at ascending.
func (s *ChatStore) ListChatTagsByChatID(chatID int64) ([]ChatTag, error) {
	var tags []ChatTag
	err := s.db.Select(&tags,
		`SELECT id, chat_id, tag, create_at
		 FROM chat_tags
		 WHERE chat_id = ?
		 ORDER BY create_at ASC`,
		chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_chat_tags_failed"), err)
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
		return fmt.Errorf("%s: %w", i18n.T("db_delete_chat_tag_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_chat_tag_not_found"), id)
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
		return fmt.Errorf("%s (id=%d): %w", i18n.T("db_delete_chat_tags_for_chat_failed"), chatID, err)
	}
	return nil
}
