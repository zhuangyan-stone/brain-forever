// migrate-category-to-taged 数据迁移工具
// 将 chat_sessions 表的 category(int) 列迁移为 taged(bool) 列
//   - category > 0 → taged = 1
//   - category = 0 → taged = 0
//   - 最终删除旧的 category 列
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

	// 检查 chat_sessions 表是否存在
	var tbl string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='chat_sessions'`).Scan(&tbl); err != nil {
		return fmt.Errorf("chat_sessions 表不存在")
	}

	// 读取当前表的所有列
	cols := map[string]bool{}
	infoRows, err := db.Query(`PRAGMA table_info(chat_sessions)`)
	if err != nil {
		return fmt.Errorf("查询列信息失败: %w", err)
	}
	for infoRows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		infoRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
		cols[name] = true
	}
	infoRows.Close()

	if !cols["category"] {
		fmt.Println("  ℹ️ 旧列 category 不存在，无需迁移")
		return nil
	}

	if cols["taged"] {
		fmt.Println("  ℹ️ 新列 taged 已存在，跳过添加步骤")
	} else {
		fmt.Println("  ➕ 添加 taged 列...")
		if _, err := db.Exec(`ALTER TABLE chat_sessions ADD COLUMN taged INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("添加 taged 列失败: %w", err)
		}
		fmt.Println("  ✅ taged 列已添加")
	}

	// 迁移数据：category > 0 → taged = 1
	fmt.Println("  🔄 迁移数据: category > 0 → taged = 1 ...")
	result, err := db.Exec(`UPDATE chat_sessions SET taged = 1 WHERE category > 0`)
	if err != nil {
		return fmt.Errorf("数据迁移失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	fmt.Printf("  ✅ %d 条记录已标记为 taged = 1\n", affected)

	// 尝试删除旧的 category 列
	fmt.Println("  🗑️ 删除旧的 category 列...")
	if _, err := db.Exec(`ALTER TABLE chat_sessions DROP COLUMN category`); err != nil {
		fmt.Printf("  ⚠️ 删除 category 列失败 (需要 SQLite 3.35.0+): %v\n", err)
		fmt.Println("  ℹ️  旧的 category 列会被新代码忽略（不在 SELECT 列表中）")
	} else {
		fmt.Println("  ✅ category 列已删除")
	}

	return nil
}
