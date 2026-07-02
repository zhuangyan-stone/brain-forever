// migrate-keywords-drop-updateat 数据迁移工具
// 删除 keywords 表中多余的 update_at 字段及相关触发器。
//
// 历史原因：旧版 schema 在 keywords 中包含了 update_at 列和对应的触发器，
// 新版 schema 已移除，此工具用于清理已有数据库中的残留字段和触发器。
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
		// 跳过 WAL/SHM 文件
		if strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, "-wal") {
			continue
		}
		if !strings.HasSuffix(name, ".brain.db") {
			continue
		}
		dbFiles = append(dbFiles, filepath.Join("localdb", name))
	}

	if len(dbFiles) == 0 {
		fmt.Println("⚠️  未找到 *.brain.db")
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
	// 规范化路径：SQLite 使用正斜杠
	normalized := strings.ReplaceAll(dbPath, "\\", "/")
	db, err := sql.Open("sqlite3", normalized+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer db.Close()

	// 检查 keywords 表是否存在
	var tbl string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='keywords'`).Scan(&tbl); err != nil {
		fmt.Println("  ℹ️  keywords 表不存在，跳过")
		return nil
	}

	// 检查 keywords 表是否有 update_at 列
	hasUpdateAt, err := hasColumn(db, "keywords", "update_at")
	if err != nil {
		return err
	}
	if !hasUpdateAt {
		fmt.Println("  ℹ️  update_at 列不存在，无需迁移")
		return nil
	}

	// 删除 trg_keywords_update_at 触发器
	fmt.Println("  🗑️  删除触发器 trg_keywords_update_at...")
	if _, err := db.Exec("DROP TRIGGER IF EXISTS trg_keywords_update_at"); err != nil {
		fmt.Printf("  ⚠️  删除触发器失败: %v\n", err)
	} else {
		fmt.Println("  ✅ 触发器 trg_keywords_update_at 已删除")
	}

	// 删除 update_at 列
	fmt.Println("  🗑️  删除 update_at 列...")
	if _, err := db.Exec(`ALTER TABLE keywords DROP COLUMN update_at`); err != nil {
		fmt.Printf("  ⚠️  删除 update_at 列失败 (需要 SQLite 3.35.0+): %v\n", err)
		fmt.Println("  ℹ️  旧的 update_at 列会被新代码忽略（不在 INSERT 中）")
	} else {
		fmt.Println("  ✅ update_at 列已删除")
	}

	return nil
}

// hasColumn 检查表是否存在指定列名。
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
