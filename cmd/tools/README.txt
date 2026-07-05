cmd/tools — 数据库迁移 & 辅助工具
======================================

按时间先后次序排列：

----------------------------------------------------------------------
1.  extract-glyph
    字体图标提取工具。
    从字体文件中提取字形轮廓数据，生成 SVG 图标。
    非数据库迁移工具。

    位置: cmd/tools/extract-glyph/main.go

----------------------------------------------------------------------
2.  migrate-extracted-count                      (2026-06-27)
    迁移 chat_sessions 表的 extracted_message_count 列。
    - 列重命名: extracted_message_count → extracted_count
    - 值修正: 从 traits 表统计每个 chat 的特征数，写入 extracted_count

    位置: cmd/tools/migrate-extracted-count/main.go

----------------------------------------------------------------------
3.  migrate-category-to-taged                     (2026-06-30)
    迁移 chat_sessions 表的 category(int) 列。
    - category > 0  → taged = 1
    - category = 0  → taged = 0
    - 最终删除旧的 category 列

    位置: cmd/tools/migrate-category-to-taged/main.go

----------------------------------------------------------------------
4.  migrate-chat-id-to-sn                         (2026-07-01)
    【最终废弃】见 7. migrate-chat-sn-to-id

    将 chat_messages、web_sources、chat_tags 三张表的
    chat_id (INTEGER FK→chat_sessions.id)
    迁移为 chat_sn (TEXT FK→chat_sessions.sn)。
    - 添加 chat_sn TEXT 列
    - 通过 JOIN chat_sessions 填充 chat_sn
    - 创建新索引，删除旧索引和旧的 chat_id 列
    - 需要 SQLite 3.35.0+ 支持 DROP COLUMN

    位置: cmd/tools/migrate-chat-id-to-sn/main.go

----------------------------------------------------------------------
5.  migrate-keywords-drop-updateat                 (2026-07-02)
    删除 keywords 表中多余的 update_at 字段及相关触发器。
    历史原因: 旧版 schema 在 keywords 中包含 update_at 列和触发器，
    新版 schema 已移除，此工具用于清理已有数据库中的残留字段和触发器。

    位置: cmd/tools/migrate-keywords-drop-updateat/main.go

----------------------------------------------------------------------
6.  migrate-chat-tags-drop-updateat                (2026-07-02)
    删除 chat_tags 表中多余的 update_at 字段及相关索引。
    历史原因: 旧版 schema 在 chat_tags 中包含 update_at 列和索引，
    新版 schema 已移除，此工具用于清理已有数据库中的残留字段和索引。

    位置: cmd/tools/migrate-chat-tags-drop-updateat/main.go

----------------------------------------------------------------------
7.  migrate-chat-sn-to-id                          (2026-07-05)
    正是 4. migrate-chat-id-to-sn 的逆操作

    将 chat_messages、web_sources、chat_tags、chat_favorites 四张表的
    chat_sn (TEXT FK→chat_sessions.sn)
    迁移回 chat_id (INTEGER FK→chat_sessions.id)。
    - 此操作是 #4 (migrate-chat-id-to-sn) 的逆向操作，最终废弃了 #4
    - 采用重建表方式，确保 chat_id 位于 id 之后的第二列
    - 通过 JOIN chat_sessions ON sn 填充 chat_id
    - chat_sessions.sn 保留不变（前端需要）

    位置: cmd/tools/migrate-chat-sn-to-id/main.go

======================================================================

运行方式:
  所有 Go 工具均需要 CGO_ENABLED=1（因为依赖 go-sqlite3）。
  建议在项目根目录下使用 b.bat 配置的环境运行，例如:

      cd cmd/tools/migrate-chat-sn-to-id
      go run main.go

  或从项目根目录:

      $env:CGO_ENABLED=1; go run cmd/tools/migrate-chat-sn-to-id/main.go
