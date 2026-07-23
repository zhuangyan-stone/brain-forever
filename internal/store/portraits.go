package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"BrainForever/infra/zylog"

	"github.com/jmoiron/sqlx"
)

// ============================================================
// UserPortrait — persisted user portrait (AI impression)
// ============================================================

// UserPortrait represents a row in the user_portraits table.
// JSONB columns (core_traits, key_highlights, hot_tags) are stored as
// json.RawMessage and serialized/deserialized in the store layer.
type UserPortrait struct {
	ID              int64           `db:"id"`
	UserID          int64           `db:"user_id"`
	Title           string          `db:"title"`
	Content         string          `db:"content"`
	CoreTraits      json.RawMessage `db:"core_traits"`    // JSONB
	KeyHighlights   json.RawMessage `db:"key_highlights"` // JSONB
	HotTags         json.RawMessage `db:"hot_tags"`       // JSONB
	HottestTag      string          `db:"hottest_tag"`
	HottestTagCount int             `db:"hottest_tag_count"`
	ChatCount       int             `db:"chat_count"`
	TraitCount      int             `db:"trait_count"`
	SpanDays        int             `db:"span_days"`
	EarliestDate    *time.Time      `db:"earliest_date"`
	LatestDate      *time.Time      `db:"latest_date"`
	Retouch         int             `db:"retouch"`
	CreatedAt       time.Time       `db:"created_at"`
}

// ============================================================
// HotTagItem — a single hot tag entry
// ============================================================

// HotTagItem represents a single tag with its count in the hot_tags JSONB array.
type HotTagItem struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ============================================================
// PortraitStore
// ============================================================

// PortraitStore provides access to the user_portraits table.
type PortraitStore struct {
	logger zylog.Logger
}

// NewPortraitStore creates a new PortraitStore.
func NewPortraitStore(logger zylog.Logger) *PortraitStore {
	return &PortraitStore{
		logger: zylog.WrapWithSubject(logger, "store-portrait"),
	}
}

// db returns the global PostgreSQL connection.
func (s *PortraitStore) db() *sqlx.DB {
	return ThePGDB()
}

// ============================================================
// CRUD methods
// ============================================================

// GetLatestPortrait returns the most recent portrait for a user.
// Returns nil and no error if no record exists yet.
func (s *PortraitStore) GetLatestPortrait(userID int64) (*UserPortrait, error) {
	sqlStr := `SELECT id, user_id, title, content,
		          core_traits, key_highlights, hot_tags,
		          hottest_tag, hottest_tag_count,
		          chat_count, trait_count, span_days,
		          earliest_date, latest_date, retouch, created_at
		   FROM user_portraits
		   WHERE user_id = $1
		   ORDER BY created_at DESC
		   LIMIT 1`
	var row UserPortrait
	err := s.db().Get(&row, sqlStr, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to get latest portrait. %w", err)
	}
	return &row, nil
}

// InsertPortrait saves a new portrait record. All historical records are kept.
// Returns the new record ID.
func (s *PortraitStore) InsertPortrait(p *UserPortrait) (int64, error) {
	sqlStr := `INSERT INTO user_portraits
		(user_id, title, content,
		 core_traits, key_highlights, hot_tags,
		 hottest_tag, hottest_tag_count,
		 chat_count, trait_count, span_days,
		 earliest_date, latest_date, retouch)
		VALUES($1, $2, $3,
		       $4::jsonb, $5::jsonb, $6::jsonb,
		       $7, $8,
		       $9, $10, $11,
		       $12, $13, $14)
		RETURNING id, created_at`

	// Marshal JSONB fields.
	coreTraitsJSON := marshalJSON(p.CoreTraits, "[]")
	keyHighlightsJSON := marshalJSON(p.KeyHighlights, "[]")
	hotTagsJSON := marshalJSON(p.HotTags, "[]")

	var id int64
	var createdAt time.Time
	err := s.db().QueryRow(sqlStr,
		p.UserID, p.Title, p.Content,
		coreTraitsJSON, keyHighlightsJSON, hotTagsJSON,
		p.HottestTag, p.HottestTagCount,
		p.ChatCount, p.TraitCount, p.SpanDays,
		p.EarliestDate, p.LatestDate, p.Retouch,
	).Scan(&id, &createdAt)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d title=%s]:\n%v", sqlStr, p.UserID, p.Title, err)
		return 0, fmt.Errorf("failed to insert portrait. %w", err)
	}
	return id, nil
}

// ListPortraits returns all portraits for a user, newest first.
func (s *PortraitStore) ListPortraits(userID int64, limit int, offset int) ([]UserPortrait, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	sqlStr := `SELECT id, user_id, title, content,
		          core_traits, key_highlights, hot_tags,
		          hottest_tag, hottest_tag_count,
		          chat_count, trait_count, span_days,
		          earliest_date, latest_date, retouch, created_at
		   FROM user_portraits
		   WHERE user_id = $1
		   ORDER BY created_at DESC
		   LIMIT $2 OFFSET $3`
	var rows []UserPortrait
	err := s.db().Select(&rows, sqlStr, userID, limit, offset)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d limit=%d offset=%d]:\n%v",
			sqlStr, userID, limit, offset, err)
		return nil, fmt.Errorf("failed to list portraits. %w", err)
	}
	return rows, nil
}

// Close is a no-op because PortraitStore does not own a connection.
func (s *PortraitStore) Close() error {
	return nil
}

// marshalJSON marshals a byte slice as a JSON string for PostgreSQL JSONB.
// Returns the fallback if the input is nil or empty.
func marshalJSON(data json.RawMessage, fallback string) string {
	if len(data) == 0 {
		return fallback
	}
	return string(data)
}
