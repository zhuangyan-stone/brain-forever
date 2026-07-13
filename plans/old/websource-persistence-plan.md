# WebSource 持久化方案

## 背景

`agent.Message.Sources`（`[]toolimp.WebSource`）目前不持久化到 DB。`callLLMWithPipeline` 返回的 `*Message` 中携带了 `Sources`（来自 web search 结果），但 `persistMessageToDB` 只写入了 `content` 和 `reasoning`，忽略了 `Sources`。导致页面刷新后前端无法恢复 WebSources 面板。

v3 设计文档已规划了 `web_sources` 表，但从未实现。Phase B 移除了内存中的 `chat.messages`，使得 WebSource 持久化更加必要——因为现在所有数据都来自 DB。

## 设计

### 1. store 层：新增 `WebSource` 结构体和 `web_sources` 表

```go
// internal/store/chats.go 新增
type WebSource struct {
    ID          int64   `db:"id"`
    SessionID   int64   `db:"session_id"`
    MsgID       int64   `db:"msg_id"`       // 指向 chat_messages.id（不是 group_index）
    Title       string  `db:"title"`
    Content     string  `db:"content"`
    URL         string  `db:"url"`
    SiteName    string  `db:"site_name"`
    SiteIcon    string  `db:"site_icon"`
    PublishDate string  `db:"publish_date"`
    Score       float64 `db:"score"`
    CreateAt    string  `db:"create_at"`
}
```

**注意**：`MsgID` 指向 `chat_messages.id`（自增主键），而不是 `group_index`。因为同一条 assistant 消息（同一个 `group_index`）可能对应多条 `chat_messages` 记录（如 tool call 中间消息），而 web sources 只关联最终的 assistant 回复。

但实际上，`persistMessageToDB` 插入 assistant 消息后，我们可以获取到刚插入的 `chat_messages.id`（通过 `LastInsertId`）。但当前 `InsertMessage` 没有返回 ID。

**简化方案**：`MsgID` 使用 `group_index`（即 `agent.Message.ID`），因为：
- 前端 `sources-panel` 通过 `msg_id`（即 `group_index`）关联消息
- 查询时按 `session_id + group_index` 即可获取
- 不需要修改 `InsertMessage` 的返回值

### 2. DB schema 变更

在 `initSchema` 中新增 `web_sources` 表：

```sql
CREATE TABLE IF NOT EXISTS web_sources (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   INTEGER NOT NULL REFERENCES chat_sessions(id),
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
CREATE INDEX IF NOT EXISTS idx_web_sources_session_msg 
    ON web_sources(session_id, msg_id);
```

### 3. store 层新增方法

在 `internal/store/messages.go` 中新增：

```go
// InsertWebSources 批量插入 web sources
func (s *ChatStore) InsertWebSources(sessionID int64, msgID int64, sources []toolimp.WebSource) error

// ListWebSourcesBySession 按 session_id 查出所有 sources，按 msg_id 分组返回
func (s *ChatStore) ListWebSourcesBySession(sessionID int64) (map[int64][]toolimp.WebSource, error)

// DeleteWebSourcesByGroup 删除某 group_index 的所有 web sources
func (s *ChatStore) DeleteWebSourcesByGroup(sessionID int64, groupIndex int) error
```

### 4. 持久化时机：`persistMessageToDB`

修改 `persistMessageToDB`，在插入 assistant 消息后，如果 `msg.Sources` 不为空，则调用 `InsertWebSources` 持久化。

```go
func persistMessageToDB(session *session, msg *Message) {
    // ... 现有逻辑：插入 chat_messages ...
    
    // 新增：持久化 WebSources
    if len(msg.Sources) > 0 {
        if err := session.chatStore.InsertWebSources(dbSessionID, int64(groupIndex), msg.Sources); err != nil {
            log.Printf("failed to persist web sources for user %s: %v", session.userNo, err)
        }
    }
}
```

### 5. 读取时恢复 Sources：修改 `convertDBMessagesToAgentMessages`

当前 `convertDBMessagesToAgentMessages` 不填充 `Sources`。需要改为：

1. 先查出所有 messages
2. 再查出所有 web sources（按 session_id）
3. 按 `group_index` 匹配后填充

但 `convertDBMessagesToAgentMessages` 当前只接收 `[]store.Message`，没有 `session_id` 参数。需要修改签名。

**方案 A**：修改 `convertDBMessagesToAgentMessages` 签名，增加 `chatStore` 和 `sessionID` 参数

```go
func convertDBMessagesToAgentMessages(
    dbMessages []store.Message, 
    chatStore *store.ChatStore, 
    sessionID int64,
) []Message
```

内部调用 `chatStore.ListWebSourcesBySession(sessionID)` 获取所有 sources，然后按 `group_index` 匹配。

**方案 B**：在调用方（`GetMessages`、`OnSwitchChat`、`OnProposeChatTitle`）先查出 sources，再传入

方案 A 更简洁，减少重复代码。

### 6. 删除时级联：修改 `DeleteMessageGroup`

当删除消息组时，也需要删除对应的 web sources。

修改 `store.DeleteMessageGroup`，或者修改 `SessionManager.DeleteMessage` 在调用 `DeleteMessageGroup` 之前先调用 `DeleteWebSourcesByGroup`。

**选择**：在 `SessionManager.DeleteMessage` 中先删除 web sources，再删除 messages。因为 `store` 层的方法应该保持单一职责。

### 7. 影响范围

| 文件 | 修改内容 |
|------|---------|
| `internal/store/chats.go` | 新增 `WebSource` 结构体；`initSchema` 新增 `web_sources` 表 |
| `internal/store/messages.go` | 新增 `InsertWebSources`、`ListWebSourcesBySession`、`DeleteWebSourcesByGroup` |
| `internal/agent/db.go` | `persistMessageToDB` 增加 WebSources 持久化逻辑 |
| `internal/agent/types.go` | `convertDBMessagesToAgentMessages` 增加 `chatStore` 和 `sessionID` 参数；更新所有调用方 |
| `internal/agent/on_chat.go` | `OnSwitchChat` 中调用 `convertDBMessagesToAgentMessages` 的地方适配新签名 |
| `internal/agent/on_title.go` | `OnProposeChatTitle` 中调用 `convertDBMessagesToAgentMessages` 的地方适配新签名 |
| `internal/agent/on_msg_del.go` | `SessionManager.DeleteMessage` 增加 web sources 删除（已在 types.go 中） |

### 8. 执行步骤

1. **store 层**：新增 `WebSource` 结构体 + `initSchema` 建表 + 三个方法
2. **agent 层**：修改 `persistMessageToDB` 增加 WebSources 持久化
3. **agent 层**：修改 `convertDBMessagesToAgentMessages` 签名，增加 sources 加载
4. **agent 层**：更新所有调用方适配新签名
5. **agent 层**：`SessionManager.DeleteMessage` 增加 web sources 删除
6. **构建测试**

### 9. 注意事项

- `web_sources` 表使用 `CREATE TABLE IF NOT EXISTS`，已有 DB 文件不会自动添加该表。需要手动删除 `.db` 文件或添加迁移逻辑。
- 当前方案使用 `group_index`（即 `agent.Message.ID`）作为 `msg_id`，与前端 `sources-panel` 的 `msg_id` 字段一致。
- `toolimp.WebSource` 定义在 `internal/agent/toolimp/web_search.go`，store 层不能直接引用 agent 包。需要在 store 层也定义 `WebSource` 结构体，或者在 store 层使用通用类型。
  - **解决方案**：store 层定义自己的 `WebSource` 结构体，`InsertWebSources` 接收 `[]store.WebSource`。在 `persistMessageToDB` 中做转换。
