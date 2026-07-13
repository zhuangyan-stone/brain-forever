package store

import (
	"fmt"
	"time"
)

// ============================================================
// Chat Favorites CRUD
// ============================================================

// FavoriteItem represents a favorited chat session with an optional custom tag.
type FavoriteItem struct {
	ID int64 `db:"id" json:"id"`

	ChatID    int64  `db:"chat_id" json:"chat_id"`
	CustomTag string `db:"custom_tag" json:"custom_tag"`

	CreateAt time.Time `db:"create_at" json:"create_at"`
	UpdateAt time.Time `db:"update_at" json:"update_at"`
}

// InsertFavoriteItem inserts a new favorite record for the given chat ID.
func (s *ChatStore) InsertFavoriteItem(chatID int64, customTag string) error {
	sqlStr := `INSERT INTO chat_favorites(chat_id, custom_tag)
		 VALUES($1, $2)`
	_, err := s.db().Exec(sqlStr, chatID, customTag)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return fmt.Errorf("failed to insert favorite. %w", err)
	}
	return nil
}

// UpdateFavoriteItemsCustomTag renames all occurrences of oldCustomTag to newCustomTag.
func (s *ChatStore) UpdateFavoriteItemsCustomTag(oldCustomTag, newCustomTag string) (int64, error) {
	sqlStr := `UPDATE chat_favorites
		 SET custom_tag = $1
		 WHERE custom_tag = $2`
	result, err := s.db().Exec(sqlStr, newCustomTag, oldCustomTag)
	if err != nil {
		s.logger.Errorf("SQL [%s]:\n%v", sqlStr, err)
		return 0, fmt.Errorf("failed to update custom tag. %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows, nil
}

// UpdateFavoriteItemChatCustomTag updates the custom_tag of a single favorite record.
func (s *ChatStore) UpdateFavoriteItemChatCustomTag(id int64, oldCustomTag, newCustomTag string) error {
	sqlStr := `UPDATE chat_favorites
		 SET custom_tag = $1
		 WHERE id = $2 AND custom_tag = $3`
	result, err := s.db().Exec(sqlStr, newCustomTag, id, oldCustomTag)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to update favorite item custom tag. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("favorite not found (id=%d, old_custom_tag=%s)", id, oldCustomTag)
	}
	return nil
}

// IsExistsFavoriteItem checks whether a favorite record exists.
func (s *ChatStore) IsExistsFavoriteItem(chatID int64, customTag string) (bool, error) {
	sqlStr := "SELECT COUNT(1) FROM chat_favorites WHERE chat_id = $1 AND custom_tag = $2"
	var exists bool
	err := s.db().Get(&exists, sqlStr, chatID, customTag)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return false, fmt.Errorf("failed to check favorite existence. %w", err)
	}
	return exists, nil
}

// DeleteFavoriteItem deletes a favorite record.
func (s *ChatStore) DeleteFavoriteItem(chatID int64, customTag string) error {
	sqlStr := `DELETE FROM chat_favorites
		 WHERE chat_id = $1 AND custom_tag = $2`
	result, err := s.db().Exec(sqlStr, chatID, customTag)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return fmt.Errorf("failed to delete favorite. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("favorite not found (chat_id=%d, custom_tag=%s)", chatID, customTag)
	}
	return nil
}

// DeleteFavoriteItemsByChatID deletes all favorite records for the given chat ID.
func (s *ChatStore) DeleteFavoriteItemsByChatID(chatID int64) (int64, error) {
	sqlStr := "DELETE FROM chat_favorites WHERE chat_id = $1"
	result, err := s.db().Exec(sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return 0, fmt.Errorf("failed to delete favorite. %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows, nil
}

// FavoritedChatTitleTag represents a chat session that has been favorited.
type FavoritedChatTitleTag struct {
	SN        string `db:"sn" json:"sn"`
	Title     string `db:"title" json:"title"`
	CustomTag string `db:"custom_tag" json:"custom_tag"`

	CreateAt time.Time `db:"create_at" json:"create_at"`
	UpdateAt time.Time `db:"update_at" json:"update_at"`
}

// SelectFavoritedChatTitlesGroupByTags queries all favorited chat sessions for a user.
func (s *ChatStore) SelectFavoritedChatTitlesGroupByTags(userID int64) (map[string][]FavoritedChatTitleTag, error) {
	sqlStr := `SELECT cs.sn, cs.title, cf.custom_tag, cs.create_at, cs.update_at
		 FROM chat_sessions cs
		 JOIN chat_favorites cf ON cs.id = cf.chat_id
		 WHERE cs.user_id = $1 AND cs.deleted = FALSE
		 ORDER BY cf.custom_tag, cs.update_at DESC, cs.create_at DESC`
	var rows []FavoritedChatTitleTag
	err := s.db().Select(&rows, sqlStr, userID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to select favorited chat titles. %w", err)
	}

	result := make(map[string][]FavoritedChatTitleTag)
	for _, r := range rows {
		result[r.CustomTag] = append(result[r.CustomTag], r)
	}
	return result, nil
}
