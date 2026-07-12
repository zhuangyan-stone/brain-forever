// migrate-traits-privacy-level 数据迁移工具
// 为 localdb/0000/ 下所有 *.brain.db 的 traits 表添加 privacy_level 列
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
	dir := filepath.Join("localdb", "0000")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取 %s 目录失败: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, "-wal") {
			continue
		}
		if !strings.HasSuffix(name, ".brain.db") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}

	if len(files) == 0 {
		fmt.Println("⚠️  未找到 *.brain.db")
		return nil
	}

	for _, f := range files {
		fmt.Printf("\n📂 %s\n", f)
		if err := migrateOne(f); err != nil {
			fmt.Fprintf(os.Stderr, "  ❌ %v\n", err)
		}
	}
	return nil
}

func migrateOne(path string) error {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer db.Close()

	// 检查 traits 表是否存在
	var tbl string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='traits'`).Scan(&tbl); err != nil {
		fmt.Println("  ℹ️ traits 表不存在，跳过")
		return nil
	}

	// 检查 privacy_level 列是否已存在
	cols := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(traits)`)
	if err != nil {
		return fmt.Errorf("查询列信息失败: %w", err)
	}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
		cols[name] = true
	}
	rows.Close()

	colName := "privacy_level"
	if cols[colName] {
		fmt.Printf("  ℹ️ %s 列已存在，跳过\n", colName)
		return nil
	}

	// 添加 privacy_level 列
	_, err = db.Exec(fmt.Sprintf(`ALTER TABLE traits ADD COLUMN %s INTEGER NOT NULL DEFAULT 1`, colName))
	if err != nil {
		return fmt.Errorf("添加 %s 列失败: %w", colName, err)
	}
	fmt.Printf("  ✅ 添加 %s 列成功\n", colName)
	return nil
}
