# chat_id → chat_sn 迁移计划

## 审查结论

### remote-server（internal/remote/agent/）
- **无需修改**。remote-server 是无状态服务，通过 HTTP 接收 local-server 发送的数据。所有请求和响应结构体都不包含 `chat_id`，使用 `SN` 作为会话标识。

### 前端（frontend/static/）
- **无需修改**。搜索 `chat_id` 在所有 `.js` 文件中返回 0 结果。前端已全部使用 `sn` 作为对话标识与后端通信。

### local-server ↔ remote-server 通信
- **无需修改**。`traitsRemoteRequest` 结构体使用 `SN string` 字段（见 `on_traits.go:48`），不使用 `chat_id`。

### local-server ↔ 前端通信
- **无需修改**。所有 API 端点使用 `sn` 参数，`Chat` 结构体的 JSON 序列化使用 `sn` 字段。

## 背景

目前 `chat_messages`、`web_sources`、`chat_tags` 三张表通过 `chat_id`（INTEGER）外键关联到 `chat_sessions(id)`。而 `chat_favorites` 表已经使用 `chat_sn`（TEXT）关联到 `chat_sessions(sn)`。

为了统一关联方式并解耦自增 ID，需要将这 3 张表也改为使用 `chat_sn`（TEXT）关联。`chat_sessions.sn` 是全局唯一字符串（格式如 `chat-<uuid-v4>`），更适合作为跨系统关联键。

## 影响范围

### 1. 数据库层（store 包） — 表结构变更

#### 1.1 chat_messages 表

| 变更 | 说明 |
|------|------|
| 删 `chat_id INTEGER NOT NULL REFERENCES chat_sessions(id)` | 移除旧外键 |
| 增 `chat_sn TEXT NOT NULL REFERENCES chat_sessions(sn)` | 添加新外键 |
| 增索引 `idx_chat_messages_chat_sn ON chat_messages(chat_sn)` | 保证查询性能 |

#### 1.2 web_sources 表

| 变更 | 说明 |
|------|------|
| 删 `chat_id INTEGER NOT NULL REFERENCES chat_sessions(id)` | 移除旧外键 |
| 增 `chat_sn TEXT NOT NULL REFERENCES chat_sessions(sn)` | 添加新外键 |
| 删索引 `idx_web_sources_chat_msg ON web_sources(chat_id, msg_id)` | 旧索引 |
| 增索引 `idx_web_sources_chat_msg ON web_sources(chat_sn, msg_id)` | 新索引 |

#### 1.3 chat_tags 表

| 变更 | 说明 |
|------|------|
| 删 `chat_id INTEGER NOT NULL REFERENCES chat_sessions(id)` | 移除旧外键 |
| 增 `chat_sn TEXT NOT NULL REFERENCES chat_sessions(sn)` | 添加新外键 |
| 删索引 `idx_chat_tags_chat_id ON chat_tags(chat_id)` | 旧索引 |
| 增索引 `idx_chat_tags_chat_sn ON chat_tags(chat_sn)` | 新索引 |

### 2. Go 结构体变更（chats.go）

#### Message 结构体
```go
// 删除
ChatID int64 `db:"chat_id"`
// 增加
ChatSN string `db:"chat_sn"`
```

#### WebSource 结构体
```go
// 删除
ChatID int64 `db:"chat_id"`
// 增加
ChatSN string `db:"chat_sn"`
```

#### ChatTag 结构体
```go
// 删除
ChatID int64 `db:"chat_id" json:"chat_id"`
// 增加
ChatSN string `db:"chat_sn" json:"chat_sn"`
```

### 3. store 包方法变更

以下所有方法中，参数 `chatID int64` 改为 `chatSN string`，SQL 中 `chat_id` 改为 `chat_sn`，SELECT 列表中 `chat_id` 改为 `chat_sn`。

#### chats.go 中的变更

| 方法 | 变更 |
|------|------|
| `PhysicalDelete(id int, sn string)` | 内联 SQL: `chat_tags WHERE chat_id = ?` → `chat_tags WHERE chat_sn = ?`; `web_sources WHERE chat_id = ?` → `web_sources WHERE chat_sn = ?`; `chat_messages WHERE chat_id = ?` → `chat_messages WHERE chat_sn = ?` |
| `EmptyTrash()` | 同上，所有 `chat_id = ?` 改为 `chat_sn = ?`，但需要先获取 sn |
| `SelectChatTitleTagsGroup()` | `JOIN chat_tags ct ON cs.id = ct.chat_id` → `JOIN chat_tags ct ON cs.sn = ct.chat_sn` |

#### messages.go 中的变更

| 方法 | 当前签名 | 新签名 |
|------|---------|-------|
| `InsertMessage` | `(chatID int64, groupIndex int, ...)` | `(chatSN string, groupIndex int, ...)` |
| `ListMessages` | `(chatID int64)` | `(chatSN string)` |
| `ListMessagesByRange` | `(chatID int64, startID int64, limit int)` | `(chatSN string, startID int64, limit int)` |
| `ListUnExtractMessages` | `(chatID int64)` | `(chatSN string)` |
| `CountMessages` | `(chatID int64)` | `(chatSN string)` |
| `DeleteMessageGroup` | `(chatID int64, groupIndex int)` | `(chatSN string, groupIndex int)` |
| `InsertWebSources` | `(chatID int64, msgID int64, sources []WebSource)` | `(chatSN string, msgID int64, sources []WebSource)` |
| `ListWebSourcesByChat` | `(chatID int64)` | `(chatSN string)` |

内部 SQL 变更示例：
```sql
-- 旧
INSERT INTO chat_messages(chat_id, group_index, ...) VALUES(?, ?, ...)
WHERE chat_id = ?
SELECT id, chat_id, group_index, ...
-- 新
INSERT INTO chat_messages(chat_sn, group_index, ...) VALUES(?, ?, ...)
WHERE chat_sn = ?
SELECT id, chat_sn, group_index, ...
```

#### tags.go 中的变更

| 方法 | 当前签名 | 新签名 |
|------|---------|-------|
| `InsertChatTag` | `(chatID int64, tag string)` | `(chatSN string, tag string)` |
| `ListChatTagsByChatID` | → 改名为 `ListChatTagsByChatSN` | `(chatSN string)` |
| `DeleteChatTagsByChatID` | → 改名为 `DeleteChatTagsByChatSN` | `(chatSN string)` |

### 4. agent 包变更

#### 4.1 agent/db.go — persistMessageToDB

```go
// 旧签名
func persistMessageToDB(session *session, msg *Message, chatID int64)
// 新签名
func persistMessageToDB(session *session, msg *Message, chatSN string)
```

内部变更：
- `session.chatsStore.InsertMessage(chatID, ...)` → `InsertMessage(chatSN, ...)`
- `store.WebSource{ChatID: chatID, ...}` → `store.WebSource{ChatSN: chatSN, ...}`
- `session.chatsStore.InsertWebSources(chatID, ...)` → `InsertWebSources(chatSN, ...)`
- `session.chatsStore.TouchChat(chatID)` → `TouchChat(chatSN)`（如 TouchChat 也改为 chatSN）
- in-memory list 查找 `c.ID == chatID` → `c.SN == chatSN`

#### 4.2 agent/types.go

**convertDBMessagesToAgentMessages:**
```go
// 旧签名
func convertDBMessagesToAgentMessages(dbMessages []store.Message, chatStore *store.ChatStore, chatID int64)
// 新签名
func convertDBMessagesToAgentMessages(dbMessages []store.Message, chatStore *store.ChatStore, chatSN string)
```
- `chatStore.ListWebSourcesByChat(chatID)` → `ListWebSourcesByChat(chatSN)`

**loadMessagesAsLLMMessages:**
```go
// 内部
chatID := s.currentChat.dbChat.ID  // old
chatSN := s.currentChat.dbChat.SN  // new
```

**syncCurrentChatTitleToChatList:**
```go
// 内部用 chatID 查找
chatID := s.currentChat.dbChat.ID  // old
chatSN := s.currentChat.dbChat.SN  // new
// in-memory list 查找
s.chats[i].ID == chatID  // old
s.chats[i].SN == chatSN  // new
```

**DeleteMessage (在 SessionManager 上):**
```go
chatID := s.currentChat.dbChat.ID  // old
chatSN := s.currentChat.dbChat.SN  // new
return s.chatsStore.DeleteMessageGroup(chatID, int(msgID))  // old
return s.chatsStore.DeleteMessageGroup(chatSN, int(msgID))  // new
```

#### 4.3 agent/on_msg_new.go

**appendNewRequestMessage:**
```go
dbSessionID = session.currentChat.dbChat.ID  // old
// 调用链：
persistMessageToDB(session, reqMsg, chatID)  // old
// new: 需要传递 chatSN
```

**OnNewMessage 中:**
```go
msgChatID := session.currentChat.dbChat.ID  // old
msgChatSN := session.currentChat.dbChat.SN  // new
```

#### 4.4 agent/on_tag.go

所有 `dbSessionID`（int64）的使用改为 `chatSN`（string）：

| 位置 | 旧 | 新 |
|------|----|-----|
| 查找 chat | `c.ID` 作为 `dbSessionID` | 直接用 `chatSN`（已有） |
| `ListChatTagsByChatID(dbSessionID)` | 需要 chatID | `ListChatTagsByChatSN(chatSN)` |
| `CountMessages(dbSessionID)` | 需要 chatID | `CountMessages(chatSN)` |
| `DeleteChatTagsByChatID(dbSessionID)` | 需要 chatID | `DeleteChatTagsByChatSN(chatSN)` |
| `InsertChatTag(dbSessionID, ...)` | 需要 chatID | `InsertChatTag(chatSN, ...)` |
| `UpdateChatTag(dbSessionID, true)` | 需要 chatID | 改为 `UpdateChatTagBySN(chatSN, true)`（或保留 UpdateChatTag 但改为 sn 参数） |

#### 4.5 agent/on_title.go

```go
// dbSessionID = c.ID  →  直接用 chatSN
dbMessages, err := session.chatsStore.ListMessages(dbSessionID)  // old
dbMessages, err := session.chatsStore.ListMessages(chatSN)  // new
agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, session.chatsStore, dbSessionID)  // old
agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, session.chatsStore, chatSN)  // new
```

#### 4.6 agent/on_chat.go

**OnPermanentDelete:**
```go
// PhysicalDelete 签名是 (id int, sn string)
// 内部已改为 chat_sn 后，可以简化为只需 sn
chatStore.PhysicalDelete(int(chatID), sn)  // old
// PhysicalDelete 改为只需 sn
chatStore.PhysicalDelete(sn)  // new
```

**OnChatPin:**
```go
session.chatsStore.UpdateChatPin(targetChat.ID, pinned)  // old
session.chatsStore.UpdateChatPinBySN(sn, pinned)  // new
```

#### 4.7 agent/toolimp/chat_samples.go

```go
// 字段
chatID int64  // old → 删除
chatSN string // 已有 chatSN 字段，可直接使用

// Execute 方法中:
dbMessages, err := f.chatsStore.ListMessagesByRange(f.chatID, ...)  // old
dbMessages, err := f.chatsStore.ListMessagesByRange(f.chatSN, ...)  // new

// 不再需要 chatID 解析逻辑（FindChatBySN 步骤可移除）
```

#### 4.8 agent/on_msg_new.go 中的 `persistMessageToDB` 调用

```go
// 在 appendNewRequestMessage 中:
chatID := session.currentChat.dbChat.ID  // old
persistMessageToDB(session, reqMsg, chatID)  // old
// new:
chatSN := session.currentChat.dbChat.SN
persistMessageToDB(session, reqMsg, chatSN)

// 在 OnNewMessage 中:
msgChatID := session.currentChat.dbChat.ID  // old
persistMessageToDB(session, assistantMsg, msgChatID)  // old
// new:
msgChatSN := session.currentChat.dbChat.SN
persistMessageToDB(session, assistantMsg, msgChatSN)
```

### 5. 数据库迁移工具

创建 `cmd/tools/migrate-chat-id-to-sn/main.go`，参考 `cmd/tools/migrate-category-to-taged/main.go` 的样式。

迁移逻辑针对每个 `.chats.db` 文件：

1. 检查 `chat_messages` 表是否有 `chat_sn` 列，若无则添加
2. 检查 `web_sources` 表是否有 `chat_sn` 列，若无则添加
3. 检查 `chat_tags` 表是否有 `chat_sn` 列，若无则添加
4. 从 `chat_sessions` 表读取 `id` → `sn` 映射
5. 更新三个表的 `chat_sn` 字段：`UPDATE chat_messages SET chat_sn = (SELECT sn FROM chat_sessions WHERE id = chat_messages.chat_id)`
6. 验证数据完整性（无 NULL chat_sn）
7. 删除旧的 `chat_id` 列（SQLite 3.35.0+ 支持 DROP COLUMN）
8. 重建索引（删除旧索引，创建新索引）
9. 触发器的 REFERENCES 也需要更新 — 但触发器在 scheme.go 中定义，迁移工具不需要处理触发器（因为 `CREATE TRIGGER IF NOT EXISTS` 会在下次 schema 初始化时使用新的 SQL）

## 执行顺序

建议按以下顺序实施，每个步骤编译通过后再进行下一步：

```
Step 1:  scheme.go — 修改表 DDL
Step 2:  chats.go — 修改结构体定义
Step 3:  messages.go — 修改所有方法
Step 4:  tags.go — 修改所有方法
Step 5:  chats.go 中的 PhysicalDelete/EmptyTrash/SelectChatTitleTagsGroup
Step 6:  agent/db.go — persistMessageToDB
Step 7:  agent/types.go — 辅助函数
Step 8:  agent/on_tag.go — handler
Step 9:  agent/on_msg_new.go, on_title.go, on_chat.go
Step 10: agent/toolimp/chat_samples.go
Step 11: 创建迁移工具
Step 12: 全局编译验证
```

## 风险与注意事项

1. **SQLite ALTER TABLE 限制**：SQLite 不支持 `ALTER TABLE ... DROP COLUMN` 之前的版本（<3.35.0）。迁移工具需要使用 `DROP COLUMN` 并处理失败回退。
2. **现有数据库兼容**：`initSchema` 使用 `CREATE TABLE IF NOT EXISTS`，已存在的表不会重新创建。因此 schema 中的新 DDL 只对新数据库生效。旧数据库通过迁移工具处理。
3. **外键约束**：SQLite 默认不启用外键约束（`PRAGMA foreign_keys = OFF`）。改为 `chat_sn` 后外键引用 `chat_sessions(sn)`，需要确保 `sn` 列有 UNIQUE 索引（已有）。
4. **chat_favorites 表**：已经使用 `chat_sn`，无需修改，保持一致。
5. **traits 表**：已使用 `chat_sn`，无需修改。
6. **`TouchChat` 方法**：目前使用 `id` 参数更新 `chat_sessions`，因为 `chat_sessions` 仍然保留自增 `id`。该方法的参数不需要改为 `chatSN`（仍然用 `id` 更高效）。但 agent 层调用 `TouchChat` 的地方需要传递 `dbChat.ID` 而不是 `dbChat.SN`。
