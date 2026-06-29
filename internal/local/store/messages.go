package store

import (
	"fmt"
)

// InsertMessage records a new message.
func (s *ChatStore) InsertMessage(chatID int64, groupIndex int, role int,
	content string, reasoning *string, interrupted int) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_messages(chat_id, group_index, role, reasoning, content, interrupted)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		chatID, groupIndex, role, reasoning, content, interrupted,
	)
	if err != nil {
		return fmt.Errorf("failed to insert message. %w", err)
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
		return nil, fmt.Errorf("failed to list un-extracted messages: %w", err)
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
		return fmt.Errorf("failed to begin transaction. %w", err)
	}
	defer tx.Rollback()

	// Delete web sources first (foreign key or not, clean up orphans)
	_, err = tx.Exec(
		"DELETE FROM web_sources WHERE chat_id = ? AND msg_id = ?",
		chatID, groupIndex,
	)
	if err != nil {
		return fmt.Errorf("failed to delete web sources for message group. %w", err)
	}

	// Delete messages
	_, err = tx.Exec(
		"DELETE FROM chat_messages WHERE chat_id = ? AND group_index = ?",
		chatID, groupIndex,
	)
	if err != nil {
		return fmt.Errorf("failed to delete message group. %w", err)
	}

	return tx.Commit()
}

// ============================================================
// WebSources CRUD
// ============================================================

// InsertWebSources batch-inserts web sources for a given message group.
// Each source is associated with the chat and message group index.
func (s *ChatStore) InsertWebSources(chatID int64, msgID int64, sources []WebSource) error {
	if len(sources) == 0 {
		return nil
	}

	for _, src := range sources {
		_, err := s.db.Exec(
			`INSERT INTO web_sources(chat_id, msg_id, title, content, url,
			                         site_name, site_icon, publish_date, score)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			chatID, msgID,
			src.Title, src.Content, src.URL,
			src.SiteName, src.SiteIcon, src.PublishDate, src.Score,
		)
		if err != nil {
			return fmt.Errorf("failed to insert web source. %w", err)
		}
	}
	return nil
}

// ListWebSourcesByChat queries all web sources for a given chat,
// grouped by msg_id. Returns a map keyed by msg_id (group_index).
func (s *ChatStore) ListWebSourcesByChat(chatID int64) (map[int64][]WebSource, error) {
	var sources []WebSource
	err := s.db.Select(&sources,
		`SELECT id, chat_id, msg_id, title, content, url,
		        site_name, site_icon, publish_date, score, create_at
		 FROM web_sources
		 WHERE chat_id = ?
		 ORDER BY msg_id ASC, id ASC`,
		chatID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list web sources: %w", err)
	}

	result := make(map[int64][]WebSource, 8)
	for _, src := range sources {
		result[src.MsgID] = append(result[src.MsgID], src)
	}
	return result, nil
}
