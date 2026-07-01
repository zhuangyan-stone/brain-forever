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
	migrateChatIDToChatSN(s.db)

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

// migrateChatIDToChatSN migrates chat_messages, web_sources, chat_tags
// from chat_id (INTEGER FK→chat_sessions.id) to chat_sn (TEXT FK→chat_sessions.sn).
//
// Steps per table:
//  1. Add chat_sn column if not present
//  2. Populate chat_sn from chat_sessions.sn via chat_id join
//  3. Create new indexes (chat_sn-based)
//  4. Drop old chat_id column (SQLite 3.35.0+)
//  5. Drop old indexes
func migrateChatIDToChatSN(db *sqlx.DB) {
	type tableInfo struct {
		name        string
		oldIndex    string // old index to drop (empty=none)
		newIndexDDL string // CREATE INDEX SQL for new index
	}

	tables := []tableInfo{
		{
			name:        "chat_messages",
			oldIndex:    "", // no old index to drop for chat_messages
			newIndexDDL: "CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_sn ON chat_messages(chat_sn)",
		},
		{
			name:        "web_sources",
			oldIndex:    "idx_web_sources_chat_msg",
			newIndexDDL: "CREATE INDEX IF NOT EXISTS idx_web_sources_chat_msg ON web_sources(chat_sn, msg_id)",
		},
		{
			name:        "chat_tags",
			oldIndex:    "idx_chat_tags_chat_id",
			newIndexDDL: "CREATE INDEX IF NOT EXISTS idx_chat_tags_chat_sn ON chat_tags(chat_sn)",
		},
	}

	for _, tbl := range tables {
		// Step 1: Check if chat_id column exists (if not, already migrated)
		var hasChatID int
		err := db.QueryRow(
			`SELECT COUNT(1) FROM pragma_table_info(?) WHERE name = 'chat_id'`,
			tbl.name,
		).Scan(&hasChatID)
		if err != nil || hasChatID == 0 {
			continue // Already migrated or table doesn't exist
		}

		// Step 2: Check if chat_sn column exists, add if not
		var hasChatSN int
		db.QueryRow(
			`SELECT COUNT(1) FROM pragma_table_info(?) WHERE name = 'chat_sn'`,
			tbl.name,
		).Scan(&hasChatSN)
		if hasChatSN == 0 {
			if _, err := db.Exec(
				`ALTER TABLE ` + tbl.name + ` ADD COLUMN chat_sn TEXT NOT NULL DEFAULT ''`,
			); err != nil {
				continue
			}
		}

		// Step 3: Populate chat_sn from chat_sessions.sn
		db.Exec(
			`UPDATE ` + tbl.name + ` SET chat_sn = (
				SELECT sn FROM chat_sessions WHERE id = ` + tbl.name + `.chat_id
			) WHERE chat_sn = ''`,
		)

		// Step 4: Create new index
		if tbl.newIndexDDL != "" {
			db.Exec(tbl.newIndexDDL)
		}

		// Step 5: Drop old index (for tables that had one)
		if tbl.oldIndex != "" {
			db.Exec(`DROP INDEX IF EXISTS ` + tbl.oldIndex)
		}

		// Step 6: Drop old chat_id column
		if _, err := db.Exec(`ALTER TABLE ` + tbl.name + ` DROP COLUMN chat_id`); err != nil {
			// DROP COLUMN requires SQLite 3.35.0+; if it fails, the old column
			// will just be ignored by the new code (not in SELECT list).
		}
	}
}
