// migrate-extracted-count 数据迁移工具
// 1. chat_sessions 表列重命名：extracted_message_count → extracted_count
// 2. 值订正：从 traits 表统计每个 chat 的特征数，写入 extracted_count
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type dbPair struct{ chatsDB, brainDB, prefix string }

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

	var pairs []dbPair
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, "-wal") {
			continue
		}
		if !strings.HasSuffix(name, ".chats.db") {
			continue
		}
		prefix := strings.TrimSuffix(name, ".chats.db")
		p := dbPair{
			chatsDB: filepath.Join("localdb", name),
			brainDB: filepath.Join("localdb", prefix+".brain.db"),
			prefix:  prefix,
		}
		if _, err := os.Stat(p.brainDB); os.IsNotExist(err) {
			p.brainDB = ""
		}
		pairs = append(pairs, p)
	}

	if len(pairs) == 0 {
		fmt.Println("⚠️  未找到 *.chats.db")
		return nil
	}

	for _, p := range pairs {
		fmt.Printf("\n📂 %s\n", p.chatsDB)
		if err := migrateOne(p); err != nil {
			fmt.Fprintf(os.Stderr, "  ❌ %v\n", err)
		}
	}
	return nil
}

func migrateOne(p dbPair) error {
	// Step 1: 打开 chats.db
	chatsDB, err := sql.Open("sqlite3", p.chatsDB+"?_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("open chats.db: %w", err)
	}
	defer chatsDB.Close()

	var tbl string
	if err := chatsDB.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='chat_sessions'`).Scan(&tbl); err != nil {
		return fmt.Errorf("chat_sessions 表不存在")
	}

	// Step 2: 检查列，执行重命名
	cols := map[string]bool{}
	infoRows, err := chatsDB.Query(`PRAGMA table_info(chat_sessions)`)
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

	oldCol := "extracted_message_count"
	newCol := "extracted_count"

	if cols[oldCol] && !cols[newCol] {
		fmt.Printf("  🔄 重命名: %s → %s\n", oldCol, newCol)
		_, err := chatsDB.Exec(fmt.Sprintf(`ALTER TABLE chat_sessions RENAME COLUMN %s TO %s`, oldCol, newCol))
		if err != nil {
			fmt.Printf("  ⚠️ RENAME 失败，回退: %v\n", err)
			chatsDB.Exec(fmt.Sprintf(`ALTER TABLE chat_sessions ADD COLUMN %s INTEGER NOT NULL DEFAULT 0`, newCol))
			chatsDB.Exec(fmt.Sprintf(`UPDATE chat_sessions SET %s = %s`, newCol, oldCol))
		}
	} else if cols[newCol] {
		fmt.Printf("  ℹ️ 新列已存在\n")
	} else {
		return fmt.Errorf("既无旧列也无新列")
	}

	// Step 2b: 重命名 trait_time → extracted_at
	oldTimeCol := "trait_time"
	newTimeCol := "extracted_at"
	if cols[oldTimeCol] && !cols[newTimeCol] {
		fmt.Printf("  🔄 重命名: %s → %s\n", oldTimeCol, newTimeCol)
		_, err := chatsDB.Exec(fmt.Sprintf(`ALTER TABLE chat_sessions RENAME COLUMN %s TO %s`, oldTimeCol, newTimeCol))
		if err != nil {
			fmt.Printf("  ⚠️ RENAME 失败，回退: %v\n", err)
			chatsDB.Exec(fmt.Sprintf(`ALTER TABLE chat_sessions ADD COLUMN %s DATETIME`, newTimeCol))
			chatsDB.Exec(fmt.Sprintf(`UPDATE chat_sessions SET %s = %s`, newTimeCol, oldTimeCol))
		}
	} else if cols[newTimeCol] {
		fmt.Printf("  ℹ️ 新列 %s 已存在\n", newTimeCol)
	}

	// Step 3: 读所有 chat SN
	type chatRow struct {
		ID int64
		SN string
	}
	var chats []chatRow
	cr, err := chatsDB.Query(`SELECT id, sn FROM chat_sessions`)
	if err != nil {
		return fmt.Errorf("查询 chat_sessions 失败: %w", err)
	}
	for cr.Next() {
		var c chatRow
		cr.Scan(&c.ID, &c.SN)
		chats = append(chats, c)
	}
	cr.Close()
	fmt.Printf("  📊 %d 个 chat\n", len(chats))

	// Step 4: 从 brain.db 统计特征数
	traitCount := map[string]int{}
	if p.brainDB != "" {
		brain, err := sql.Open("sqlite3", p.brainDB+"?_journal_mode=WAL")
		if err != nil {
			fmt.Printf("  ⚠️ brain.db 打不开: %v\n", err)
		} else {
			defer brain.Close()
			var t string
			if err := brain.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='traits'`).Scan(&t); err != nil {
				fmt.Printf("  ℹ️ traits 表不存在\n")
			} else {
				for _, c := range chats {
					var n int
					brain.QueryRow(`SELECT COUNT(1) FROM traits WHERE chat_sn = ?`, c.SN).Scan(&n)
					traitCount[c.SN] = n
				}
			}
		}
	}

	// Step 5: 更新 extracted_count
	upd, err := chatsDB.Prepare(`UPDATE chat_sessions SET extracted_count = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer upd.Close()

	for _, c := range chats {
		n := traitCount[c.SN]
		upd.Exec(n, c.ID)
	}
	fmt.Printf("  ✅ 更新 %d 个 chat 的 extracted_count\n", len(chats))
	return nil
}
