# 特征增量提取细化方案

## 总览：5 个改进点

1. **未活动 Chat** — 仅用 `ExtractedAt` 是否为 null 判断
2. **活动 Chat** — 比较 `ExtractedAt` 与最后一条消息的 `CreateAt`
3. **字段改名** — `ExtractedMessageCount` → `ExtractedCount`，语义从"已提取消息数"变为"已提取特征数"
4. **条件标记消息** — 提取到新特征才标记消息为 extracted，否则不标记
5. **延续性** — 空提取后消息保持 unextracted，下次提取时 LLM 看到更多连续上下文

---

## Phase 1：字段改名 `ExtractedMessageCount` → `ExtractedCount`（Point 3）

### 1.1 后端 DB schema 变更

**文件**: [`internal/local/store/chats.go`](internal/local/store/chats.go:116)

```sql
-- 表 schema 中：extracted_message_count → extracted_count
extracted_count INTEGER NOT NULL DEFAULT 0,
```

迁移：`ALTER TABLE chat_sessions RENAME COLUMN extracted_message_count TO extracted_count;`

### 1.2 Go struct 变更

**文件**: [`internal/local/store/chats.go`](internal/local/store/chats.go:21)

```go
// 旧
ExtractedMessageCount int  `db:"extracted_message_count" json:"extracted_message_count"`

// 新
ExtractedCount int  `db:"extracted_count" json:"extracted_count"`
```

所有 SQL SELECT/UPDATE 中引用 `extracted_message_count` 的地方都要改。

### 1.3 后端方法变更

| 方法 | 文件 | 变更 |
|------|------|------|
| `UpdateExtractionProgress` | [`chats.go:432`](internal/local/store/chats.go:432) | SQL SET `extracted_count = ?` |
| `updateExtractionProgress` (helper) | [`on_traits.go:406`](internal/local/agent/on_traits.go:406) | 写入 `ExtractedCount` 为新特征数 (len(features)) 而不是 totalCount |
| `OnExtractTraits` | [`on_traits.go:142`](internal/local/agent/on_traits.go:142) | 读取 `foundChat.ExtractedCount` 写入响应 |
| `handleNoNewMessages` | [`on_traits.go:382`](internal/local/agent/on_traits.go:382) | 同上 |
| `traitsRemoteResponse` | [`on_traits.go:70`](internal/local/agent/on_traits.go:70) | `ExtractedMessageCount` → `ExtractedCount`，JSON tag 改成 `extracted_count` |

### 1.4 前端变更

**文件**: [`frontend/static/chat-list.js`](frontend/static/chat-list.js:507,635)

```javascript
// 旧
chat.extracted_message_count
// 新
chat.extracted_count
```

需要修改两处：
- L507：右键菜单提取状态判断
- L635：提取完成后更新 chat 对象

---

## Phase 2：提取条件逻辑重构（Points 1, 2）

### 2.1 前端右键菜单逻辑

**文件**: [`frontend/static/chat-list.js`](frontend/static/chat-list.js:489-529)

| 条件 | 当前行为 | 新行为 |
|------|---------|--------|
| 未活动 chat + `extracted_at == null` | 可提取 | 可提取 ✅（不变） |
| 未活动 chat + `extracted_at != null` | 禁用（已提取） | 禁用（已提取）✅（不变） |
| 活动 chat + 无提取记录 | 可提取 | 可提取 ✅ |
| 活动 chat + `extracted_at >= lastMsg.create_at` | 比较消息数 | **禁用**（最后消息时间 ≤ 提取时间） |
| 活动 chat + `extracted_at < lastMsg.create_at` | 比较消息数 | **可提取**（最后消息时间 > 提取时间） |

方案一（简单）：活动 chat 不依赖 `extracted_count`，仅靠时间戳判断。

### 2.2 后端提取触发逻辑

**文件**: [`internal/local/agent/on_traits.go`](internal/local/agent/on_traits.go:188)

当前：`len(dbMessages) == 0` → `handleNoNewMessages`

保持不变。`ListUnExtractMessages` 仍然只返回 `extracted=0` 的消息。这天然支持：
- 首次提取：全部消息
- 增量提取：仅未标记的消息

---

## Phase 3：条件标记消息（Points 4, 5）

### 3.1 后端变更

**文件**: [`internal/local/agent/on_traits.go`](internal/local/agent/on_traits.go:210-228)

当前代码：
```go
// Step 5: 存储特征
if len(remoteResp.Features) > 0 {
    h.storeTraitsInSession(...)
}

// Step 6: 更新进度 + 标记消息
totalCount := len(dbMessages)
if count, err := chatsStore.CountMessages(foundChat.ID); err == nil {
    totalCount = count
}
updateExtractionProgress(foundChat, chatsStore, totalCount)
chatsStore.MarkMessagesExtracted(foundChat.ID, lastMsgID)
```

新逻辑：
```go
// Step 5: 存储特征（仅当有特征时）
newTraitCount := len(remoteResp.Features)
if newTraitCount > 0 {
    h.storeTraitsInSession(...)
    // Step 6a: 标记本次参与提取的消息为已提取
    chatsStore.MarkMessagesExtracted(foundChat.ID, lastMsgID)
    // Step 6b: 更新 ExtractedCount（累积）
    updateExtractionProgress(foundChat, chatsStore, newTraitCount)
} else {
    // Step 6c: 不标记消息，不更新提取进度
    // 下次提取时，这些消息仍会参与，LLM 能看到更多上下文
}
```

注意 `updateExtractionProgress` 的语义变化：
- 旧：参数 `totalCount` 是消息总数
- 新：参数 `newTraitCount` 是本次新增的特征数
- 写入 DB 时：`extracted_count = extracted_count + newTraitCount`（累积）

### 3.2 `UpdateExtractionProgress` 方法变更

新方法需要**读取当前值然后累加**，或者在方法内做 `SET extracted_count = extracted_count + ?`：

```go
// 累加模式
func (s *ChatStore) AddExtractedCount(chatID int64, increment int) error {
    _, err := s.db.Exec(
        `UPDATE chat_sessions
         SET trait_time = CURRENT_TIMESTAMP,
             extracted_count = extracted_count + ?
         WHERE id = ?`,
        increment, chatID,
    )
    ...
}
```

---

## Phase 4：涉及文件完整清单

| 文件 | 变更 | 说明 |
|------|------|------|
| [`internal/local/store/chats.go`](internal/local/store/chats.go) | 修改 | struct 字段改名 + DB schema + SQL 查询 + 新增 `AddExtractedCount` 方法 |
| [`internal/local/agent/on_traits.go`](internal/local/agent/on_traits.go) | 修改 | 提取逻辑条件化 + 响应字段改名 |
| [`internal/local/agent/types.go`](internal/local/agent/types.go) | 无需修改 | session 不直接暴露提取字段 |
| [`frontend/static/chat-list.js`](frontend/static/chat-list.js) | 修改 | 字段引用改名 + 活动 chat 判断逻辑变更 |
| [`frontend/static/chat-api.js`](frontend/static/chat-api.js) | 可能需改 | 如果 trait API 响应解析有字段引用 |

---

## 执行顺序

1. **Phase 1**：后端 struct + DB schema + 所有 SQL（`extracted_message_count` → `extracted_count`）
2. **Phase 1.5**：前端字段引用改名
3. **Phase 2**：前端活动 chat 判断逻辑（时间戳比较）
4. **Phase 3**：后端条件标记消息逻辑
5. **验证**：编译 + 确认前端功能正常
