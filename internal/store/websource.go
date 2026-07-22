package store

import (
	"fmt"
	"time"
)

// WebSource represents a web search result source stored in the database.
type WebSource struct {
	ID          int64     `db:"id" json:"id"`
	ChatID      int64     `db:"chat_id" json:"chat_id"`
	MsgID       int64     `db:"msg_id" json:"msg_id"`
	Title       string    `db:"title"`
	Content     string    `db:"content"`
	URL         string    `db:"url"`
	SiteName    string    `db:"site_name"`
	SiteIcon    string    `db:"site_icon"`
	PublishDate string    `db:"publish_date"`
	Score       float64   `db:"score"`
	CreateAt    time.Time `db:"create_at"`
}

// InsertWebSources batch-inserts web sources for a given message group.
func (s *ChatStore) InsertWebSources(chatID int64, msgID int64, sources []WebSource) error {
	if len(sources) == 0 {
		return nil
	}

	sqlStr := `INSERT INTO web_sources(chat_id, msg_id, title, content, url,
			                         site_name, site_icon, publish_date, score)
			 VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	for _, src := range sources {
		_, err := s.db().Exec(sqlStr,
			chatID, msgID,
			src.Title, src.Content, src.URL,
			src.SiteName, src.SiteIcon, src.PublishDate, src.Score,
		)
		if err != nil {
			s.logger.Errorf("sQL [%s] args=[chatID=%d msgID=%d]:\n%v", sqlStr, chatID, msgID, err)
			return fmt.Errorf("failed to insert web source. %w", err)
		}
	}
	return nil
}

// ListWebSourcesByChat queries all web sources for a given chat, grouped by msg_id.
func (s *ChatStore) ListWebSourcesByChat(chatID int64) (map[int64][]WebSource, error) {
	sqlStr := `SELECT id, chat_id, msg_id, title, content, url,
		        site_name, site_icon, publish_date, score, create_at
		 FROM web_sources
		 WHERE chat_id = $1
		 ORDER BY msg_id ASC, id ASC`
	var sources []WebSource
	err := s.db().Select(&sources, sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("sQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return nil, fmt.Errorf("failed to list web sources. %w", err)
	}

	result := make(map[int64][]WebSource, 8)
	for _, src := range sources {
		result[src.MsgID] = append(result[src.MsgID], src)
	}
	return result, nil
}
