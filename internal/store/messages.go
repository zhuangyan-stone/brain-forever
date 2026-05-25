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

// ============================================================
// WebSources CRUD
// ============================================================

// InsertWebSources batch-inserts web sources for a given message group.
// Each source is associated with the session and message group index.
func (s *ChatStore) InsertWebSources(sessionID int64, msgID int64, sources []WebSource) error {
	if len(sources) == 0 {
		return nil
	}

	for _, src := range sources {
		_, err := s.db.Exec(
			`INSERT INTO web_sources(session_id, msg_id, title, content, url,
			                         site_name, site_icon, publish_date, score)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID, msgID,
			src.Title, src.Content, src.URL,
			src.SiteName, src.SiteIcon, src.PublishDate, src.Score,
		)
		if err != nil {
			return fmt.Errorf("failed to insert web source. %w", err)
		}
	}
	return nil
}

// ListWebSourcesBySession queries all web sources for a given session,
// grouped by msg_id. Returns a map keyed by msg_id (group_index).
func (s *ChatStore) ListWebSourcesBySession(sessionID int64) (map[int64][]WebSource, error) {
	var sources []WebSource
	err := s.db.Select(&sources,
		`SELECT id, session_id, msg_id, title, content, url,
		        site_name, site_icon, publish_date, score, create_at
		 FROM web_sources
		 WHERE session_id = ?
		 ORDER BY msg_id ASC, id ASC`,
		sessionID,
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

// DeleteWebSourcesByGroup deletes all web sources for a given message group.
func (s *ChatStore) DeleteWebSourcesByGroup(sessionID int64, groupIndex int) error {
	_, err := s.db.Exec(
		"DELETE FROM web_sources WHERE session_id = ? AND msg_id = ?",
		sessionID, groupIndex,
	)
	if err != nil {
		return fmt.Errorf("failed to delete web sources by group. %w", err)
	}
	return nil
}
