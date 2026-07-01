package store

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// initSchema initializes chat-related table schemas and runs migrations.
func (s *ChatStore) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			sn        TEXT    NOT NULL UNIQUE,
			role_no   INTEGER NOT NULL,
			title     TEXT    NOT NULL DEFAULT '',
			title_state INTEGER NOT NULL DEFAULT 0,
			extract_mode       INTEGER NOT NULL DEFAULT 0,
			extracted_at       DATETIME,
			extracted_count    INTEGER NOT NULL DEFAULT 0,
			deleted   INTEGER NOT NULL DEFAULT 0,
			pinned    INTEGER NOT NULL DEFAULT 0,
			taged     INTEGER NOT NULL DEFAULT 0,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS chat_messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_sn    TEXT    NOT NULL REFERENCES chat_sessions(sn),
			group_index INTEGER NOT NULL,
			role       INTEGER NOT NULL,
			reasoning    TEXT,
			content    TEXT    NOT NULL,
			extracted  INTEGER NOT NULL DEFAULT 0,
			interrupted INTEGER NOT NULL DEFAULT 0,
			create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_sn
			ON chat_messages(chat_sn);

		CREATE TABLE IF NOT EXISTS web_sources (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_sn      TEXT    NOT NULL REFERENCES chat_sessions(sn),
			msg_id       INTEGER NOT NULL,
			title        TEXT    NOT NULL DEFAULT '',
			content      TEXT    NOT NULL DEFAULT '',
			url          TEXT    NOT NULL DEFAULT '',
			site_name    TEXT    NOT NULL DEFAULT '',
			site_icon    TEXT    NOT NULL DEFAULT '',
			publish_date TEXT    NOT NULL DEFAULT '',
			score        REAL    NOT NULL DEFAULT 0,
			create_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_web_sources_chat_msg
			ON web_sources(chat_sn, msg_id);

		CREATE TABLE IF NOT EXISTS chat_tags (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_sn   TEXT    NOT NULL REFERENCES chat_sessions(sn),
			tag       TEXT    NOT NULL,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_chat_tags_chat_sn
			ON chat_tags(chat_sn);

		CREATE INDEX IF NOT EXISTS idx_chat_tags_tag
			ON chat_tags(tag);

		CREATE TABLE IF NOT EXISTS chat_favorites (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_sn    TEXT    NOT NULL REFERENCES chat_sessions(sn),
			custom_tag TEXT    NOT NULL DEFAULT '',
			create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_chat_favorites_chat_sn
			ON chat_favorites(chat_sn);

		CREATE INDEX IF NOT EXISTS idx_chat_favorites_custom_tag
			ON chat_favorites(custom_tag);

		CREATE TRIGGER IF NOT EXISTS trg_chat_sessions_update_at
			BEFORE UPDATE ON chat_sessions
			FOR EACH ROW
		BEGIN
			UPDATE chat_sessions SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;

		CREATE TRIGGER IF NOT EXISTS trg_chat_messages_update_at
			BEFORE UPDATE ON chat_messages
			FOR EACH ROW
		BEGIN
			UPDATE chat_messages SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;

		CREATE TRIGGER IF NOT EXISTS trg_chat_tags_update_at
			BEFORE UPDATE ON chat_tags
			FOR EACH ROW
		BEGIN
			UPDATE chat_tags SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;

		CREATE TRIGGER IF NOT EXISTS trg_chat_favorites_update_at
			BEFORE UPDATE ON chat_favorites
			FOR EACH ROW
		BEGIN
			UPDATE chat_favorites SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to initialize chat tables. %w", err)
	}

	// Run backward-compatibility migrations
	migrateExtractedMessageCount(s.db)
	migrateTraitTime(s.db)
	migrateCategoryToTaged(s.db)

	return nil
}

// migrateExtractedMessageCount renames extracted_message_count to extracted_count.
func migrateExtractedMessageCount(db *sqlx.DB) {
	var oldCol string
	err := db.QueryRow(
		`SELECT name FROM pragma_table_info('chat_sessions') WHERE name = 'extracted_message_count'`,
	).Scan(&oldCol)
	if err != nil {
		return
	}
	if _, err := db.Exec(`ALTER TABLE chat_sessions RENAME COLUMN extracted_message_count TO extracted_count`); err != nil {
		db.Exec(`ALTER TABLE chat_sessions ADD COLUMN extracted_count INTEGER NOT NULL DEFAULT 0`)
		db.Exec(`UPDATE chat_sessions SET extracted_count = extracted_message_count`)
	}
}

// migrateTraitTime renames trait_time to extracted_at.
func migrateTraitTime(db *sqlx.DB) {
	var oldCol string
	err := db.QueryRow(
		`SELECT name FROM pragma_table_info('chat_sessions') WHERE name = 'trait_time'`,
	).Scan(&oldCol)
	if err != nil {
		return
	}
	if _, err := db.Exec(`ALTER TABLE chat_sessions RENAME COLUMN trait_time TO extracted_at`); err != nil {
		db.Exec(`ALTER TABLE chat_sessions ADD COLUMN extracted_at DATETIME`)
		db.Exec(`UPDATE chat_sessions SET extracted_at = trait_time`)
	}
}

// migrateCategoryToTaged renames category(int) to taged(bool).
//
// category > 0 → taged = 1, category = 0 → taged = 0
func migrateCategoryToTaged(db *sqlx.DB) {
	var oldCol string
	err := db.QueryRow(
		`SELECT name FROM pragma_table_info('chat_sessions') WHERE name = 'category'`,
	).Scan(&oldCol)
	if err != nil {
		return
	}
	// Add taged column (safe if schema already created it: IF NOT EXISTS not supported for ALTER ADD)
	if _, err := db.Exec(`ALTER TABLE chat_sessions ADD COLUMN taged INTEGER NOT NULL DEFAULT 0`); err != nil {
		// Column may already exist from schema init, that's fine
	}
	// Migrate data: any previously categorized chat becomes tagged
	db.Exec(`UPDATE chat_sessions SET taged = 1 WHERE category > 0`)
	// Drop the old category column
	if _, err := db.Exec(`ALTER TABLE chat_sessions DROP COLUMN category`); err != nil {
		// DROP COLUMN requires SQLite 3.35.0+; if it fails, the old column
		// will just be ignored by the new code (not in SELECT list).
	}
}
