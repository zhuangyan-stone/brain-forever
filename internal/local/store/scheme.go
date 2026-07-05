package store

import (
	"fmt"

	"BrainForever/infra/i18n"
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
			chat_id    INTEGER NOT NULL REFERENCES chat_sessions(id),
			group_index INTEGER NOT NULL,
			role       INTEGER NOT NULL,
			reasoning    TEXT,
			content    TEXT    NOT NULL,
			extracted  INTEGER NOT NULL DEFAULT 0,
			interrupted INTEGER NOT NULL DEFAULT 0,
			create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_id
			ON chat_messages(chat_id);

		CREATE TABLE IF NOT EXISTS web_sources (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id      INTEGER NOT NULL REFERENCES chat_sessions(id),
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
			ON web_sources(chat_id, msg_id);

		CREATE TABLE IF NOT EXISTS chat_tags (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id   INTEGER NOT NULL REFERENCES chat_sessions(id),
			tag       TEXT    NOT NULL,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_chat_tags_chat_id
			ON chat_tags(chat_id);

		CREATE INDEX IF NOT EXISTS idx_chat_tags_tag
			ON chat_tags(tag);

		CREATE TABLE IF NOT EXISTS chat_favorites (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id    INTEGER NOT NULL REFERENCES chat_sessions(id),
			custom_tag TEXT    NOT NULL DEFAULT '',
			create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_favorites_unique
			ON chat_favorites(chat_id, custom_tag);

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

		CREATE TRIGGER IF NOT EXISTS trg_chat_favorites_update_at
			BEFORE UPDATE ON chat_favorites
			FOR EACH ROW
		BEGIN
			UPDATE chat_favorites SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_init_chat_tables_failed"), err)
	}

	return nil
}
