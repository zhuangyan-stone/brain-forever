// migrate-chat-sn-to-id 数据迁移工具
// 将 chat_messages、web_sources、chat_tags、chat_favorites 四张表的
// chat_sn (TEXT FK→chat_sessions.sn) 迁移为 chat_id (INTEGER FK→chat_sessions.id)。
//
// 迁移步骤：
//  1. 为每张表添加 chat_id 列（若不存在）
//  2. 通过 JOIN chat_sessions 更新 chat_id 值
//  3. 创建基于 chat_id 的新索引
//  4. 删除旧的 chat_sn 列（需要 SQLite 3.35.0+）
//  5. 删除旧索引
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

// tableMigrateInfo describes the migration steps for one table.
type tableMigrateInfo struct {
	name         string // table name
	oldIndexName string // old index to drop (empty = skip)
	newIndexDDL  string // SQL to create new index (empty = skip)
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

	// Check SQLite version (DROP COLUMN requires 3.35.0+)
	var sqliteVersion string
	if err := db.QueryRow(`SELECT sqlite_version()`).Scan(&sqliteVersion); err == nil {
		fmt.Printf("  ℹ️  SQLite version: %s\n", sqliteVersion)
		needed := "3.35.0"
		if compareVersions(sqliteVersion, needed) < 0 {
			fmt.Printf("  ⚠️  SQLite %s+ 才支持 DROP COLUMN，当前为 %s，将跳过删除旧列步骤\n", needed, sqliteVersion)
			fmt.Println("  ℹ️  旧的 chat_sn 列会被新代码忽略（不在 SELECT 列表中）")
		}
	}

	// Check if chat_sessions table exists
	var tbl string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='chat_sessions'`).Scan(&tbl); err != nil {
		return fmt.Errorf("chat_sessions 表不存在")
	}

	tables := []tableMigrateInfo{
		{
			name:         "chat_messages",
			oldIndexName: "idx_chat_messages_chat_sn",
			newIndexDDL:  "CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_id ON chat_messages(chat_id)",
		},
		{
			name:         "web_sources",
			oldIndexName: "", // idx_web_sources_chat_msg 是 (chat_sn, msg_id) 复合索引，稍后重建
			newIndexDDL:  "CREATE INDEX IF NOT EXISTS idx_web_sources_chat_msg ON web_sources(chat_id, msg_id)",
		},
		{
			name:         "chat_tags",
			oldIndexName: "idx_chat_tags_chat_sn",
			newIndexDDL:  "CREATE INDEX IF NOT EXISTS idx_chat_tags_chat_id ON chat_tags(chat_id)",
		},
		{
			name:         "chat_favorites",
			oldIndexName: "", // idx_chat_favorites_unique 是 (chat_sn, custom_tag) 复合索引，稍后重建
			newIndexDDL:  "CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_favorites_unique ON chat_favorites(chat_id, custom_tag)",
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

// migrateTable performs migration for a single table.
func migrateTable(db *sql.DB, ti tableMigrateInfo) error {
	name := ti.name
	fmt.Printf("  🔄 处理 %s...\n", name)

	// Step 1: Check if chat_sn column exists (if not, already migrated)
	hasChatSN, err := hasColumn(db, name, "chat_sn")
	if err != nil {
		return err
	}
	if !hasChatSN {
		fmt.Printf("  ℹ️  %s 没有 chat_sn 列，无需迁移\n", name)
		return nil
	}

	// Step 2: Add chat_id column if not present
	hasChatID, err := hasColumn(db, name, "chat_id")
	if err != nil {
		return err
	}
	if !hasChatID {
		fmt.Printf("  ➕ 添加 chat_id 列...\n")
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN chat_id INTEGER NOT NULL DEFAULT 0", name)); err != nil {
			return fmt.Errorf("添加 chat_id 列失败: %w", err)
		}
		fmt.Println("  ✅ chat_id 列已添加")
	} else {
		fmt.Println("  ℹ️  chat_id 列已存在")
	}

	// Step 3: Populate chat_id from chat_sessions.id
	fmt.Println("  🔄 填充 chat_id 数据...")
	result, err := db.Exec(fmt.Sprintf(
		`UPDATE %s SET chat_id = (SELECT id FROM chat_sessions WHERE sn = %s.chat_sn) WHERE chat_id = 0`,
		name, name,
	))
	if err != nil {
		return fmt.Errorf("填充 chat_id 数据失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	fmt.Printf("  ✅ %d 条记录已更新 chat_id\n", affected)

	// Step 4: Verify no zero chat_id values remain
	var zeroCount int
	err = db.QueryRow(fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE chat_id = 0", name)).Scan(&zeroCount)
	if err == nil && zeroCount > 0 {
		fmt.Printf("  ⚠️  仍有 %d 条记录的 chat_id 为 0（可能 chat_sn 在 chat_sessions 中找不到对应记录）\n", zeroCount)
	}

	// Step 5: Create new index
	if ti.newIndexDDL != "" {
		fmt.Println("  🔄 创建新索引...")
		if _, err := db.Exec(ti.newIndexDDL); err != nil {
			return fmt.Errorf("创建新索引失败: %w", err)
		}
		fmt.Println("  ✅ 新索引已创建")
	}

	// Step 6: Drop old index
	if ti.oldIndexName != "" {
		fmt.Println("  🗑️ 删除旧索引...")
		if _, err := db.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", ti.oldIndexName)); err != nil {
			fmt.Printf("  ⚠️  删除旧索引 %s 失败: %v\n", ti.oldIndexName, err)
		} else {
			fmt.Println("  ✅ 旧索引已删除")
		}
	}

	// Step 7: Drop old chat_sn column
	fmt.Println("  🗑️ 删除旧的 chat_sn 列...")
	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN chat_sn", name)); err != nil {
		fmt.Printf("  ⚠️  删除 chat_sn 列失败 (需要 SQLite 3.35.0+): %v\n", err)
		fmt.Println("  ℹ️  旧的 chat_sn 列会被新代码忽略（不在 SELECT 列表中）")
	} else {
		fmt.Println("  ✅ chat_sn 列已删除")
	}

	fmt.Printf("  ✅ %s 迁移完成\n", name)
	return nil
}

// compareVersions compares two semver version strings (e.g., "3.35.0").
// Returns -1 if v1 < v2, 0 if equal, 1 if v1 > v2.
func compareVersions(v1, v2 string) int {
	parse := func(v string) []int {
		var parts []int
		for _, s := range strings.Split(v, ".") {
			var n int
			fmt.Sscanf(s, "%d", &n)
			parts = append(parts, n)
		}
		// Pad to at least 3 parts
		for len(parts) < 3 {
			parts = append(parts, 0)
		}
		return parts
	}
	a, b := parse(v1), parse(v2)
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
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
