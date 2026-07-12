package store

import (
	"fmt"
)

// initSchema initializes the traits, keywords, and vector index tables.
func (s *BrainStore) initSchema() error {
	var vecVersion string
	if err := s.db.QueryRow("SELECT vec_version()").Scan(&vecVersion); err != nil {
		return fmt.Errorf("sqlite-vec not loaded correctly: %w", err)
	}
	s.logger.Infof("sqlite-vec version: %s", vecVersion)

	schema := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS trait_vectors
		USING vec0(
			embedding float[%d] distance_metric=cosine
		);

		CREATE TABLE IF NOT EXISTS traits (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			trait          TEXT    NOT NULL,
			category       INTEGER NOT NULL,
			confidence     INTEGER NOT NULL,
			half_life      INTEGER NOT NULL,
			privacy_level  INTEGER NOT NULL DEFAULT 0,
			chat_sn        TEXT    NOT NULL DEFAULT '',
			create_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS keywords (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			word      TEXT    NOT NULL,
			kind      INTEGER NOT NULL,
			trait_id  INTEGER NOT NULL REFERENCES traits(id) ON DELETE CASCADE,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_traits_category   ON traits(category);
		CREATE INDEX IF NOT EXISTS idx_traits_half_life  ON traits(half_life);
		CREATE INDEX IF NOT EXISTS idx_traits_create_at  ON traits(create_at);
		CREATE INDEX IF NOT EXISTS idx_traits_chat_sn    ON traits(chat_sn);

		CREATE INDEX IF NOT EXISTS idx_keywords_trait_id ON keywords(trait_id);
		CREATE INDEX IF NOT EXISTS idx_keywords_word     ON keywords(word);
		CREATE INDEX IF NOT EXISTS idx_keywords_kind     ON keywords(kind);
		CREATE INDEX IF NOT EXISTS idx_keywords_trait_kind ON keywords(trait_id, kind);

		CREATE TRIGGER IF NOT EXISTS trg_traits_update_at
			BEFORE UPDATE ON traits
			FOR EACH ROW 
		BEGIN
			UPDATE traits SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;
	`, s.dimension)

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	return nil
}
