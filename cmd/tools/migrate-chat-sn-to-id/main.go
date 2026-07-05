// migrate-chat-sn-to-id 数据迁移工具
// 将 chat_messages、web_sources、chat_tags、chat_favorites 四张表的
// chat_sn (TEXT FK→chat_sessions.sn) 迁移为 chat_id (INTEGER FK→chat_sessions.id)。
//
// 迁移方式：重建表（recreate table），以确保 chat_id 位于 id 之后的第二列位置。
//
// 迁移步骤：
//  1. 创建临时表（新列序：chat_id 紧随 id，去掉 chat_sn）
//  2. 通过 JOIN chat_sessions 填充 chat_id 数据
//  3. 删除旧表 + 重命名临时表
//  4. 重建索引
//
// 注意: 需要 CGO_ENABLED=1 编译运行，因为依赖 go-sqlite3。
// 建议使用 b.bat 中的环境（已配置 MinGW GCC 路径）。
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// tableMigrateInfo describes the migration for one table.
type tableMigrateInfo struct {
	name          string   // table name
	oldIndexNames []string // old indexes to drop before recreate
	createSQL     string   // CREATE TABLE for temp table (no chat_sn, chat_id after id)
	selectSQL     string   // SELECT columns from old table (with JOIN for chat_id, when chat_sn exists)
	selectSQL2    string   // SELECT when chat_sn already dropped, use existing chat_id directly
	newIndexDDLs  []string // indexes to create after rename
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	entries, err := os.ReadDir("localdb")
	if err != nil {
		return fmt.Errorf("读取 localdb 目录失败: %w", err)
	}

	var dbFiles []string
	for _, e := range entries {
		name := e.Name()
		// Skip WAL/SHM files and non-chat DB files
		if strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, "-wal") {
			continue
		}
		if !strings.HasSuffix(name, ".chats.db") {
			continue
		}
		dbFiles = append(dbFiles, filepath.Join("localdb", name))
	}

	if len(dbFiles) == 0 {
		fmt.Println("⚠️  未找到 *.chats.db")
		return nil
	}

	for _, path := range dbFiles {
		fmt.Printf("\n📂 %s\n", path)
		if err := migrateOne(path); err != nil {
			fmt.Fprintf(os.Stderr, "  ❌ %v\n", err)
		}
	}
	return nil
}

func migrateOne(dbPath string) error {
	// Normalize path: use forward slashes for SQLite
	normalized := strings.ReplaceAll(dbPath, "\\", "/")
	db, err := sql.Open("sqlite3", normalized+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer db.Close()

	// Check if chat_sessions table exists
	var tbl string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='chat_sessions'`).Scan(&tbl); err != nil {
		return fmt.Errorf("chat_sessions 表不存在")
	}

	tables := []tableMigrateInfo{
		{
			name:          "chat_messages",
			oldIndexNames: []string{"idx_chat_messages_chat_sn"},
			createSQL: `CREATE TABLE chat_messages_new (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				chat_id    INTEGER NOT NULL DEFAULT 0,
				group_index INTEGER NOT NULL,
				role       INTEGER NOT NULL,
				reasoning    TEXT,
				content    TEXT    NOT NULL,
				extracted  INTEGER NOT NULL DEFAULT 0,
				interrupted INTEGER NOT NULL DEFAULT 0,
				create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			selectSQL: `SELECT m.id, COALESCE((SELECT id FROM chat_sessions WHERE sn = m.chat_sn), 0),
			                   m.group_index, m.role, m.reasoning, m.content,
			                   m.extracted, m.interrupted, m.create_at, m.update_at
			            FROM chat_messages m`,
			selectSQL2: `SELECT m.id, m.chat_id,
			                   m.group_index, m.role, m.reasoning, m.content,
			                   m.extracted, m.interrupted, m.create_at, m.update_at
			            FROM chat_messages m`,
			newIndexDDLs: []string{
				"CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_id ON chat_messages(chat_id)",
			},
		},
		{
			name:          "web_sources",
			oldIndexNames: []string{"idx_web_sources_chat_msg"},
			createSQL: `CREATE TABLE web_sources_new (
				id           INTEGER PRIMARY KEY AUTOINCREMENT,
				chat_id      INTEGER NOT NULL DEFAULT 0,
				msg_id       INTEGER NOT NULL,
				title        TEXT    NOT NULL DEFAULT '',
				content      TEXT    NOT NULL DEFAULT '',
				url          TEXT    NOT NULL DEFAULT '',
				site_name    TEXT    NOT NULL DEFAULT '',
				site_icon    TEXT    NOT NULL DEFAULT '',
				publish_date TEXT    NOT NULL DEFAULT '',
				score        REAL    NOT NULL DEFAULT 0,
				create_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			selectSQL: `SELECT w.id, COALESCE((SELECT id FROM chat_sessions WHERE sn = w.chat_sn), 0),
			                   w.msg_id, w.title, w.content, w.url,
			                   w.site_name, w.site_icon, w.publish_date, w.score, w.create_at
			            FROM web_sources w`,
			selectSQL2: `SELECT w.id, w.chat_id,
			                   w.msg_id, w.title, w.content, w.url,
			                   w.site_name, w.site_icon, w.publish_date, w.score, w.create_at
			            FROM web_sources w`,
			newIndexDDLs: []string{
				"CREATE INDEX IF NOT EXISTS idx_web_sources_chat_msg ON web_sources(chat_id, msg_id)",
			},
		},
		{
			name:          "chat_tags",
			oldIndexNames: []string{"idx_chat_tags_chat_sn"},
			createSQL: `CREATE TABLE chat_tags_new (
				id        INTEGER PRIMARY KEY AUTOINCREMENT,
				chat_id   INTEGER NOT NULL DEFAULT 0,
				tag       TEXT    NOT NULL,
				create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			selectSQL: `SELECT t.id, COALESCE((SELECT id FROM chat_sessions WHERE sn = t.chat_sn), 0),
			                   t.tag, t.create_at
			            FROM chat_tags t`,
			selectSQL2: `SELECT t.id, t.chat_id,
			                   t.tag, t.create_at
			            FROM chat_tags t`,
			newIndexDDLs: []string{
				"CREATE INDEX IF NOT EXISTS idx_chat_tags_chat_id ON chat_tags(chat_id)",
				"CREATE INDEX IF NOT EXISTS idx_chat_tags_tag ON chat_tags(tag)",
			},
		},
		{
			name:          "chat_favorites",
			oldIndexNames: []string{"idx_chat_favorites_unique"},
			createSQL: `CREATE TABLE chat_favorites_new (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				chat_id    INTEGER NOT NULL DEFAULT 0,
				custom_tag TEXT    NOT NULL DEFAULT '',
				create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			selectSQL: `SELECT f.id, COALESCE((SELECT id FROM chat_sessions WHERE sn = f.chat_sn), 0),
			                   f.custom_tag, f.create_at, f.update_at
			            FROM chat_favorites f`,
			selectSQL2: `SELECT f.id, f.chat_id,
			                   f.custom_tag, f.create_at, f.update_at
			            FROM chat_favorites f`,
			newIndexDDLs: []string{
				"CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_favorites_unique ON chat_favorites(chat_id, custom_tag)",
			},
		},
	}

	for _, ti := range tables {
		if err := migrateTable(db, ti); err != nil {
			fmt.Fprintf(os.Stderr, "  ❌ 迁移 %s 失败: %v\n", ti.name, err)
			continue
		}
	}

	return nil
}

// migrateTable performs migration for a single table using recreate approach.
func migrateTable(db *sql.DB, ti tableMigrateInfo) error {
	name := ti.name
	fmt.Printf("  🔄 处理 %s...\n", name)

	// Determine if migration is needed: chat_sn exists, OR chat_id is not in position 1 (second column)
	needsMigrate := false

	hasChatSN, err := hasColumn(db, name, "chat_sn")
	if err != nil {
		return err
	}
	if hasChatSN {
		needsMigrate = true
	} else {
		// Already has chat_id, but check if it's in the correct position (second column, cid=1)
		pos, posErr := columnPosition(db, name, "chat_id")
		if posErr != nil {
			return posErr
		}
		if pos != 1 {
			fmt.Printf("  ℹ️  chat_id 当前在第 %d 列（应为第 2 列），需要重建\n", pos+1)
			needsMigrate = true
		}
	}

	if !needsMigrate {
		fmt.Printf("  ℹ️  %s 已是最新结构，无需迁移\n", name)
		return nil
	}

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	// Step 1: Drop old indexes
	for _, idxName := range ti.oldIndexNames {
		if _, err := tx.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", idxName)); err != nil {
			fmt.Printf("  ⚠️  删除索引 %s 失败: %v\n", idxName, err)
		}
	}

	// Step 2: Create new temp table with chat_id after id, no chat_sn
	fmt.Println("  🔄 创建新表（chat_id 在 id 之后）...")
	if _, err := tx.Exec(ti.createSQL); err != nil {
		return fmt.Errorf("创建新表失败: %w", err)
	}

	// Step 3: Copy data — use selectSQL (with JOIN) if chat_sn exists, else selectSQL2 (use existing chat_id)
	sql := ti.selectSQL
	if !hasChatSN {
		sql = ti.selectSQL2
	}
	fmt.Println("  🔄 迁移数据...")
	result, err := tx.Exec(fmt.Sprintf("INSERT INTO %s_new %s", name, sql))
	if err != nil {
		return fmt.Errorf("迁移数据失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	fmt.Printf("  ✅ %d 条记录已迁移\n", affected)

	// Step 4: Drop old table
	fmt.Println("  🗑️ 删除旧表...")
	if _, err := tx.Exec(fmt.Sprintf("DROP TABLE %s", name)); err != nil {
		return fmt.Errorf("删除旧表失败: %w", err)
	}

	// Step 5: Rename new table
	fmt.Println("  🔄 重命名新表...")
	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s_new RENAME TO %s", name, name)); err != nil {
		return fmt.Errorf("重命名新表失败: %w", err)
	}

	// Step 6: Create new indexes
	for _, ddl := range ti.newIndexDDLs {
		if _, err := tx.Exec(ddl); err != nil {
			return fmt.Errorf("创建索引失败: %w", err)
		}
	}

	// Check for zero chat_id values
	var zeroCount int
	err = tx.QueryRow(fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE chat_id = 0", name)).Scan(&zeroCount)
	if err == nil && zeroCount > 0 {
		fmt.Printf("  ⚠️  仍有 %d 条记录的 chat_id 为 0（可能 chat_sn 在 chat_sessions 中找不到对应记录）\n", zeroCount)
	}

	// Commit
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	fmt.Printf("  ✅ %s 迁移完成\n", name)
	return nil
}

// hasColumn checks whether a table has a specific column.
func hasColumn(db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return false, fmt.Errorf("查询 %s 的列信息失败: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("读取列信息失败: %w", err)
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

// columnPosition returns the 0-based position of a column in a table.
// Returns -1 if the column is not found.
func columnPosition(db *sql.DB, tableName, columnName string) (int, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return -1, fmt.Errorf("查询 %s 的列信息失败: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return -1, fmt.Errorf("读取列信息失败: %w", err)
		}
		if name == columnName {
			return cid, nil
		}
	}
	return -1, nil
}
