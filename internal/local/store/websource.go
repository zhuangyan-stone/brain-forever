package store

import (
	"fmt"
	"time"

	"BrainForever/infra/i18n"
)

// WebSource represents a web search result source stored in the database.
// This is the store-layer equivalent of toolimp.WebSource, defined separately
// to avoid circular dependencies between store and agent packages.
type WebSource struct {
	ID          int64     `db:"id"`      // Auto-increment primary key
	ChatSN      string    `db:"chat_sn"` // References chat_sessions.sn
	MsgID       int64     `db:"msg_id"`  // Message group index (= agent.Message.ID)
	Title       string    `db:"title"`
	Content     string    `db:"content"`
	URL         string    `db:"url"`
	SiteName    string    `db:"site_name"`
	SiteIcon    string    `db:"site_icon"`
	PublishDate string    `db:"publish_date"`
	Score       float64   `db:"score"`
	CreateAt    time.Time `db:"create_at"`
}

// ============================================================
// WebSources CRUD
// ============================================================

// InsertWebSources batch-inserts web sources for a given message group.
// Each source is associated with the chat and message group index.
func (s *ChatStore) InsertWebSources(chatSN string, msgID int64, sources []WebSource) error {
	if len(sources) == 0 {
		return nil
	}

	for _, src := range sources {
		_, err := s.db.Exec(
			`INSERT INTO web_sources(chat_sn, msg_id, title, content, url,
			                         site_name, site_icon, publish_date, score)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			chatSN, msgID,
			src.Title, src.Content, src.URL,
			src.SiteName, src.SiteIcon, src.PublishDate, src.Score,
		)
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T("db_insert_web_source_failed"), err)
		}
	}
	return nil
}

// ListWebSourcesByChat queries all web sources for a given chat,
// grouped by msg_id. Returns a map keyed by msg_id (group_index).
func (s *ChatStore) ListWebSourcesByChat(chatSN string) (map[int64][]WebSource, error) {
	var sources []WebSource
	err := s.db.Select(&sources,
		`SELECT id, chat_sn, msg_id, title, content, url,
		        site_name, site_icon, publish_date, score, create_at
		 FROM web_sources
		 WHERE chat_sn = ?
		 ORDER BY msg_id ASC, id ASC`,
		chatSN,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_web_sources_failed"), err)
	}

	result := make(map[int64][]WebSource, 8)
	for _, src := range sources {
		result[src.MsgID] = append(result[src.MsgID], src)
	}
	return result, nil
}
