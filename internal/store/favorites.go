package store

import (
	"fmt"
	"time"

	"BrainForever/infra/i18n"
)

// ============================================================
// Chat Favorites CRUD
// ============================================================

// FavoriteItem represents a favorited chat session with an optional custom tag.
type FavoriteItem struct {
	ID int64 `db:"id" json:"id"` // Auto-increment primary key

	ChatID    int64  `db:"chat_id" json:"chat_id"`       // References chat_sessions.id
	CustomTag string `db:"custom_tag" json:"custom_tag"` // User-defined custom tag for grouping favorites

	CreateAt time.Time `db:"create_at" json:"create_at"`
	UpdateAt time.Time `db:"update_at" json:"update_at"`
}

// InsertFavoriteItem inserts a new favorite record for the given chat ID
// with the specified custom tag. Duplicate (chat_id, custom_tag) pairs
// are allowed since there is no UNIQUE constraint on the combination.
func (s *ChatStore) InsertFavoriteItem(chatID int64, customTag string) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_favorites(chat_id, custom_tag)
		 VALUES(?, ?)`,
		chatID, customTag,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_insert_favorite_failed"), err)
	}
	return nil
}

// UpdateFavoriteItemsCustomTag renames all occurrences of oldCustomTag to newCustomTag
// across all favorite records. Returns the number of rows updated.
func (s *ChatStore) UpdateFavoriteItemsCustomTag(oldCustomTag, newCustomTag string) (int64, error) {
	result, err := s.db.Exec(
		`UPDATE chat_favorites
		 SET custom_tag = ?
		 WHERE custom_tag = ?`,
		newCustomTag, oldCustomTag,
	)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", i18n.T("db_update_custom_tag_failed"), err)
	}
	rows, _ := result.RowsAffected()
	return rows, nil
}

// UpdateFavoriteItemChatCustomTag updates the custom_tag of a single favorite record
// identified by its primary key id, but only if its current custom_tag matches oldCustomTag.
// This acts as an optimistic lock to prevent concurrent overwrites.
// Returns an error if no matching record is found (id not found or tag mismatch).
func (s *ChatStore) UpdateFavoriteItemChatCustomTag(id int64, oldCustomTag, newCustomTag string) error {
	result, err := s.db.Exec(
		`UPDATE chat_favorites
		 SET custom_tag = ?
		 WHERE id = ? AND custom_tag = ?`,
		newCustomTag, id, oldCustomTag,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_update_favorite_item_custom_tag_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d, old_custom_tag=%s)", i18n.T("db_favorite_not_found"), id, oldCustomTag)
	}
	return nil
}

// IsExistsFavoriteItem checks whether a favorite record exists for the given
// chat ID and custom tag combination. Returns true if found, false otherwise.
func (s *ChatStore) IsExistsFavoriteItem(chatID int64, customTag string) (bool, error) {
	var exists bool
	err := s.db.Get(&exists,
		"SELECT COUNT(1) FROM chat_favorites WHERE chat_id = ? AND custom_tag = ?",
		chatID, customTag,
	)
	if err != nil {
		return false, fmt.Errorf("%s: %w", i18n.T("db_query_favorite_exists_failed"), err)
	}
	return exists, nil
}

// DeleteFavoriteItem deletes a favorite record matching the given chat ID
// and custom tag. If no matching record is found, returns an error.
func (s *ChatStore) DeleteFavoriteItem(chatID int64, customTag string) error {
	result, err := s.db.Exec(
		`DELETE FROM chat_favorites
		 WHERE chat_id = ? AND custom_tag = ?`,
		chatID, customTag,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_favorite_failed"), err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (chat_id=%d, custom_tag=%s)", i18n.T("db_favorite_not_found"), chatID, customTag)
	}
	return nil
}

// DeleteFavoriteItemsByChatID deletes all favorite records for the given chat ID.
// Returns the number of rows deleted.
func (s *ChatStore) DeleteFavoriteItemsByChatID(chatID int64) (int64, error) {
	result, err := s.db.Exec(
		`DELETE FROM chat_favorites WHERE chat_id = ?`,
		chatID,
	)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", i18n.T("db_delete_favorite_failed"), err)
	}
	rows, _ := result.RowsAffected()
	return rows, nil
}

// FavoritedChatTitleTag represents a chat session that has been favorited,
// joined with its custom tag from the chat_favorites table.
type FavoritedChatTitleTag struct {
	SN        string `db:"sn" json:"sn"`
	Title     string `db:"title" json:"title"`
	CustomTag string `db:"custom_tag" json:"custom_tag"`

	CreateAt time.Time `db:"create_at" json:"create_at"` // chat session's create_at
	UpdateAt time.Time `db:"update_at" json:"update_at"` // chat session's update_at
}

// SelectFavoritedChatTitlesGroupByTags queries all favorited (non-deleted) chat sessions,
// grouped by their custom_tag. Within each group, results are ordered by
// the chat session's update_at descending, then create_at descending.
//
// Returns a map where the key is the custom_tag value and the value is a slice
// of FavoritedChatTitleTag entries sorted as described above.
func (s *ChatStore) SelectFavoritedChatTitlesGroupByTags() (map[string][]FavoritedChatTitleTag, error) {
	var rows []FavoritedChatTitleTag
	err := s.db.Select(&rows,
		`SELECT cs.sn, cs.title, cf.custom_tag, cs.create_at, cs.update_at
		 FROM chat_sessions cs
		 JOIN chat_favorites cf ON cs.id = cf.chat_id
		 WHERE cs.deleted = 0
		 ORDER BY cf.custom_tag, cs.update_at DESC, cs.create_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_select_favorited_chat_titles_group_by_tags_failed"), err)
	}

	result := make(map[string][]FavoritedChatTitleTag)
	for _, r := range rows {
		result[r.CustomTag] = append(result[r.CustomTag], r)
	}
	return result, nil
}
