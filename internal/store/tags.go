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
		 RETURNING id, create_at`
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

// ============================================================
// Transactional tag operations
// ============================================================

// DeleteChatTagAndUpdateTaged atomically deletes a tag and, if no non-empty
// tags remain, sets chat_sessions.taged = false within a single transaction.
//
// Semantic notes:
//   - When the last non-empty tag is removed, taged=false means "was classified
//     but all tags deleted", different from taged=true (LLM processed but found
//     no suitable categories, i.e. no tag records in chat_tags).
//   - Returns remaining non-empty tag count for frontend UI state sync.
func (s *ChatStore) DeleteChatTagAndUpdateTaged(chatID int64, tag string) (int, error) {
	tx, err := s.db().Beginx()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction. %w", err)
	}
	defer tx.Rollback()

	sqlStr := "DELETE FROM chat_tags WHERE chat_id = $1 AND tag = $2"
	if _, err := tx.Exec(sqlStr, chatID, tag); err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d tag=%s]:\n%v", sqlStr, chatID, tag, err)
		return 0, fmt.Errorf("failed to delete chat tag. %w", err)
	}

	sqlStr = "SELECT COUNT(1) FROM chat_tags WHERE chat_id = $1 AND tag != ''"
	var count int
	if err := tx.Get(&count, sqlStr, chatID); err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return 0, fmt.Errorf("failed to count chat tags. %w", err)
	}

	// Only update taged=false when the last non-empty tag was removed.
	// If tags remain, taged is already true, no DB write needed.
	if count == 0 {
		sqlStr = "UPDATE chat_sessions SET taged = FALSE WHERE id = $1"
		if _, err := tx.Exec(sqlStr, chatID); err != nil {
			s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
			return 0, fmt.Errorf("failed to update chat taged flag. %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction. %w", err)
	}

	return count, nil
}

// ReplaceChatTags atomically replaces all tags for a chat and marks it as
// classified (taged=true), regardless of whether any tags were generated.
//
// Semantic notes:
//   - taged=true means "LLM has attempted classification", independent of
//     whether any rows exist in chat_tags.
//   - Non-empty tags: written to chat_tags as-is (never insert ” placeholder).
//   - Empty tags: old tags are deleted, only taged=true is set, meaning
//     "processed but no match". The distinction between "classified with tags"
//     and "classified without tags" is naturally expressed by the existence
//     of rows in chat_tags.
func (s *ChatStore) ReplaceChatTags(chatID int64, tags []string) error {
	tx, err := s.db().Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction. %w", err)
	}
	defer tx.Rollback()

	// Step 1: Delete all existing tags
	sqlStr := "DELETE FROM chat_tags WHERE chat_id = $1"
	if _, err := tx.Exec(sqlStr, chatID); err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return fmt.Errorf("failed to delete chat tags. %w", err)
	}

	// Step 2: Insert new tags (only if non-empty; never insert '' placeholder)
	for _, tag := range tags {
		sqlStr = "INSERT INTO chat_tags(chat_id, tag) VALUES($1, $2)"
		if _, err := tx.Exec(sqlStr, chatID, tag); err != nil {
			s.logger.Errorf("SQL [%s] args=[chatID=%d tag=%s]:\n%v", sqlStr, chatID, tag, err)
			return fmt.Errorf("failed to insert chat tag %q. %w", tag, err)
		}
	}

	// Step 3: Mark as classified — taged=true means LLM has processed this chat
	sqlStr = "UPDATE chat_sessions SET taged = TRUE WHERE id = $1"
	if _, err := tx.Exec(sqlStr, chatID); err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return fmt.Errorf("failed to update chat taged flag. %w", err)
	}

	return tx.Commit()
}

// DeleteChatTagByChatIDAndTag deletes a chat tag by chat_id and tag.
// Succeeds silently even if no matching row is found.
// Deprecated: Use DeleteChatTagAndUpdateTaged for atomic delete+taged update.
func (s *ChatStore) DeleteChatTagByChatIDAndTag(chatID int64, tag string) error {
	sqlStr := "DELETE FROM chat_tags WHERE chat_id = $1 AND tag = $2"
	_, err := s.db().Exec(sqlStr, chatID, tag)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d, tag=%s]:\n%v", sqlStr, chatID, tag, err)
		return fmt.Errorf("failed to delete chat tag by chat_id and tag. %w", err)
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
