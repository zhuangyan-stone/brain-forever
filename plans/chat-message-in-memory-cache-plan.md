# 当前 Chat 消息内存缓存方案

## 问题

数据库从 SQLite 切换到统一的 PostgreSQL 后，每次 AI 回复对话都需要：
1. 用户发消息 → 写入 DB（仅写入新消息，没问题）
2. **从 DB 读取全部历史消息**（`ListMessages` 全表查询 + 网络往返）
3. 发送给 LLM

对于长对话（几十甚至上百轮），每次消息往返都要全量读取，PG 的网络延迟比 SQLite 本地文件 IO 高出数倍，问题变得严重。

## 解决思路

将当前活跃 Chat (`CurrentChat`) 的消息缓存在内存中，避免每次 LLM 调用都回源查询 DB。

## 架构变更

### 1. 数据模型变更

**`internal/agent/llmtypes/types.go`** - 在 `Chat` 结构体中增加 `Messages` 字段：

```go
type Chat struct {
    DBCHat     *store.Chat
    Title      string
    TitleState TitleState
    Messages   []Message   // 新增：当前 chat 的消息内存缓存
}
```

### 2. 消息生命周期流程图

```
┌─────────────────────────────────────────────────────────────────┐
│                         OnNewMessage                            │
│                                                                 │
│  用户发消息                                                       │
│     │                                                            │
│     ▼                                                            │
│  prepareMessageForLLM                                            │
│     │  acquires Mu                                               │
│     ▼                                                            │
│  appendNewRequestMessage                                         │
│     │  ├─ 从 CurrentChat.Messages 获取 lastID (取代 DB 查询)        │
│     │  ├─ 赋新 ID, 写 DB                                          │
│     │  └─ 追加到 CurrentChat.Messages                              │
│     ▼                                                            │
│  拷贝 Messages 为 []Message (在锁内, 避免竞态)                      │
│     │                                                            │
│     ▼                                                            │
│  释放 Mu                                                         │
│     │                                                            │
│     ▼                                                            │
│  将拷贝的消息转为 []llm.Message    ◄── 不再调用 LoadMessagesAsLLMMessages│
│     │                                                            │
│     ▼                                                            │
│  callLLMWithPipeline (流式调用 LLM, 无锁)                          │
│     │                                                            │
│     ▼                                                            │
│  返回 assistantMsg                                               │
│     │                                                            │
│     ▼                                                            │
│  acquire Mu                                                      │
│     │                                                            │
│     ▼                                                            │
│  persistMessageToDB + 追加到 CurrentChat.Messages                  │
│  (仅当 CurrentChat.DBCHat.ID == msgChatID, 防止流式期间切 chat)     │
│     │                                                            │
│     ▼                                                            │
│  释放 Mu                                                         │
└─────────────────────────────────────────────────────────────────┘
```

### 3. 各入口点变更

#### 3a. `OnSwitchChat` - 切换聊天

**文件**: [`internal/agent/on_chat.go`](internal/agent/on_chat.go)

**变更**: 加载消息后，存入 `CurrentChat.Messages`

```go
// 现有代码已加载 dbMessages → agentMsgs
// 新增：
sess.Mu.Lock()
sess.User.CurrentChat.Messages = msgs  // msgs 是 []Message
sess.Mu.Unlock()
```

**时序图**:
```
OnSwitchChat
  │
  ├─ ListMessages(chatID)  ────> 从 DB 加载 (必要的, 首次加载)
  ├─ convertDBMessagesToAgentMessages
  │
  └─ CurrentChat.Messages = msgs  ────> 写入缓存
```

#### 3b. `OnNewChat` - 新建对话

**文件**: [`internal/agent/on_chat_new.go`](internal/agent/on_chat_new.go)

**变更**: 清空消息缓存

```go
sess.User.CurrentChat = &llmtypes.Chat{}  // Messages = nil (零值)
```

#### 3c. `appendNewRequestMessage` - 追加用户消息

**文件**: [`internal/agent/on_msg_new.go`](internal/agent/on_msg_new.go)

**变更**: 用缓存中的最后一条消息取代 DB 查询来获取 lastID

```go
// 原有: 调用 chatStore.ListMessages(dbChatID) 获取最后一条
// 新:
var lastID int64 = 0
if len(sess.User.CurrentChat.Messages) > 0 {
    lastMsg := sess.User.CurrentChat.Messages[len(sess.User.CurrentChat.Messages)-1]
    lastID = lastMsg.ID
}
reqMsg.ID = lastID + 1

// 写入 DB 后追加到缓存
// ... persistMessageToDB ...
sess.User.CurrentChat.Messages = append(sess.User.CurrentChat.Messages, *reqMsg)
```

#### 3d. `OnNewMessage` - 发送消息核心入口

**文件**: [`internal/agent/on_msg_new.go`](internal/agent/on_msg_new.go)

**变更**: 
1. `prepareMessageForLLM` 返回缓存消息的拷贝（锁内读取，确保一致性）
2. `OnNewMessage` 使用缓存消息不再调用 `LoadMessagesAsLLMMessages`
3. LLM 返回后，追加 assistant 消息到缓存

```go
// prepareMessageForLLM 新增返回值
func prepareMessageForLLM(...) (msgChatID int64, cachedMsgs []Message, chatCreatedSN string, chatCreatedFrontSN string, ok bool) {
    sess.Mu.Lock()
    defer sess.Mu.Unlock()
    
    // ... appendNewRequestMessage ...
    
    // 在锁内拷贝消息，避免并发竞态
    cachedMsgs = make([]Message, len(sess.User.CurrentChat.Messages))
    copy(cachedMsgs, sess.User.CurrentChat.Messages)
    
    // ...
    return msgChatID, cachedMsgs, chatCreatedSN, chatCreatedFrontSN, true
}

// OnNewMessage 使用缓存
msgChatID, cachedMsgs, ... := prepareMessageForLLM(...)

// 将缓存的 agent.Message 转为 llm.Message
llmMsgs := convertAgentMessagesToLLMMessages(cachedMsgs)

// ... 构建 messages (加上 system prompt) ...

// 调用 LLM
assistantMsg := h.callLLMWithPipeline(...)

// LLM 返回后，追加到缓存
if assistantMsg != nil {
    sess.Mu.Lock()
    persistMessageToDB(sess, assistantMsg, msgChatID, theChatStore)
    // 仅在 chat 未切换时更新缓存
    if sess.User.CurrentChat.DBCHat != nil && sess.User.CurrentChat.DBCHat.ID == msgChatID {
        sess.User.CurrentChat.Messages = append(sess.User.CurrentChat.Messages, *assistantMsg)
    }
    sess.Mu.Unlock()
}
```

#### 3e. `OnDeleteMessage` - 删除消息

**文件**: [`internal/session/manager.go`](internal/session/manager.go)

**变更**: DB 删除后，同步从缓存中移除

```go
func (m *Manager) DeleteMessage(sessionID string, msgID int64) error {
    // ... 现有查找 session 逻辑 ...
    
    s.Mu.Lock()
    defer s.Mu.Unlock()
    
    chatID := s.User.CurrentChat.DBCHat.ID
    chatStore := store.NewChatStore(m.logger)
    
    // DB 删除
    if err := chatStore.DeleteMessageGroup(chatID, int(msgID)); err != nil {
        return err
    }
    
    // 从缓存中移除该消息组 (同一 ID 的所有消息)
    kept := make([]Message, 0, len(s.User.CurrentChat.Messages))
    for _, m := range s.User.CurrentChat.Messages {
        if m.ID != msgID {
            kept = append(kept, m)
        }
    }
    s.User.CurrentChat.Messages = kept
    
    return nil
}
```

### 4. 新增辅助函数

**文件**: [`internal/agent/llmtypes/types.go`](internal/agent/llmtypes/types.go)

```go
// ConvertAgentMessagesToLLMMessages converts the agent-layer Message slice
// (from the in-memory cache) to an llm.Message slice suitable for LLM API calls.
func ConvertAgentMessagesToLLMMessages(msgs []Message) []llm.Message {
    result := make([]llm.Message, 0, len(msgs))
    for _, m := range msgs {
        result = append(result, llm.Message{Role: m.Role, Content: m.Content})
    }
    return result
}
```

**注意**: 这与现有的 `LoadMessagesAsLLMMessages` 功能类似，但入参不同（`[]Message` vs `int64` + `*ChatStore`），是独立的辅助函数。

### 5. 不变的部分（无需修改）

| 文件 | 原因 |
|------|------|
| [`internal/agent/chatllm.go`](internal/agent/chatllm.go) | `callLLMWithPipeline` 已接收 `[]llm.Message` 参数，不关心数据来源 |
| [`internal/store/messages.go`](internal/store/messages.go) | DB 层的 `ListMessages` 依然给 `OnSwitchChat` 首次加载和 trait extraction 使用 |
| [`internal/agent/on_traits.go`](internal/agent/on_traits.go) | 使用 `ListUnExtractMessages` 直接从 DB 读取未提取的消息，不走缓存 |
| [`internal/store/chats.go`](internal/store/chats.go) | 不涉及消息读取 |
| [`internal/store/pgdb.go`](internal/store/pgdb.go) | 连接池无需变更 |
| [`internal/agent/db.go`](internal/agent/db.go) | `persistMessageToDB` 不涉及缓存（调用方负责缓存一致性） |
| [`internal/agent/toolimp/chat_samples.go`](internal/agent/toolimp/chat_samples.go) | 标签生成使用 `ListMessagesByRange` 直接从 DB 分页读取，不走缓存 |
| [`internal/agent/on_tag.go`](internal/agent/on_tag.go) | 标签生成使用独立的 LLM 调用和工具，不走缓存 |

### 6. 并发安全分析

```
时间线 ──────────────────────────────────────────────────────►

Tab 1: OnNewMessage  │  锁内: 追加用户消息  │  释放锁  │  LLM 流式调用...     │  锁内: 追加assistant
                      │  拷贝缓存            │          │  (耗时, 无锁)        │  到DB和缓存
                      ▼                     ▼          ▼                     ▼
                                              ╔══════════════════════╗
                                              ║ 其他操作可能插入     ║
                                              ║ e.g. OnSwitchChat   ║
                                              ║      或             ║
                                              ║ OnDeleteMessage     ║
                                              ╚══════════════════════╝

Tab 2: OnSwitchChat   │  锁内: 替换          │
                      │  CurrentChat         │
                      ▼                     ▼
```

**关键设计决策**:
1. `prepareMessageForLLM` 在锁内完成了「追加用户消息 + 拷贝缓存」，确保了这两个操作的原子性
2. LLM 调用期间缓存可能变化（如用户切 chat），但 `callLLMWithPipeline` 使用的是拷贝后的消息副本，不受影响
3. LLM 返回后，通过对比 `msgChatID` 和 `CurrentChat.DBCHat.ID` 判断是否更新缓存，避免写错

### 7. 活动 vs 非活动 Chat 操作分析

这是架构的核心问题：有些操作对活动 chat 和非活动 chat 都支持，它们处理历史消息的方式不同。

#### 仅操作活动 chat（通过 `CurrentChat`）

| 操作 | 消息读取方式 | 本次变更 |
|------|-------------|----------|
| `OnNewMessage` - 发送消息 | 需要全部历史消息给 LLM | **改为使用缓存** |
| `OnDeleteMessage` - 删除消息 | 不需要读取，只写 DB | **改为清理缓存** |
| `OnSwitchChat` - 切换聊天 | 从 DB 加载全部消息到前端 | 流程不变，**新增缓存写入** |

这些操作的共同特点：**频繁、重复、对延迟敏感**。发送消息是用户最核心的操作，每次都要全量历史消息，优化收益最大。

#### 可操作任意 chat（通过 SN / chatID，可能活动也可能非活动）

| 操作 | 消息读取方式 | 本次变更 |
|------|-------------|----------|
| `OnExtractTraits` - 提取特征 | `ListUnExtractMessages` 从 DB 读取**未提取**的消息 | **不变** |
| `OnGenerateChatTags` - 生成标签 | `ChatSamplesTool` → `ListMessagesByRange` 从 DB **分页**读取 | **不变** |
| `OnDeleteChat` / `OnPermanentDelete` - 删除聊天 | 不涉及消息读取 | **不变** |
| `OnRestoreChat` / `OnChatPin` / 收藏操作 | 不涉及消息读取 | **不变** |

这些操作的共同特点：**一次性、低频、读模式不同**。它们不需要缓存优化，原因：

1. **`OnExtractTraits`**：仅读取 `extracted = FALSE` 的消息，过滤条件与缓存不匹配。缓存中的 `Message` 结构体没有 `extracted` 字段。该操作执行后还会调用 `UpdateMessagesExtracted` 标记已提取，这会影响缓存一致性，但当前不会走缓存。
2. **`OnGenerateChatTags`**：使用 `ChatSamplesTool` 做分页读取（每次 10 条），有游标跟踪进度。标签生成本身是一次性操作，不重复执行。而且 `ChatSamplesTool` 在任意 chat（通过 chatID）上都能工作，但缓存只有活动 chat 的。
3. **其他操作**：不涉及消息内容读取。

#### 特殊场景：`OnExtractTraits` 操作的是活动 chat

如果用户在当前 chat 上触发特征提取：
- 操作通过 SN 定位 chat，不通过 `CurrentChat`
- 从 DB 读取 `ListUnExtractMessages`，不走缓存

### 8. 内存安全

- 每个 Session 仅缓存**当前聊天**的消息，不是全部聊天
- Session GC 时会自动释放（`Chat` 及其 `Messages` 被垃圾回收）
- 如果用户有大量历史消息（极端情况），缓存会用对应内存，但这和之前 `LoadMessagesAsLLMMessages` 每次读取是等量的，只是从「反复读取」变为「常驻内存」

### 9. 实施步骤

#### Step 1: `llmtypes.Chat` 增加 `Messages` 字段 + 辅助函数
- 文件: `internal/agent/llmtypes/types.go`
- 新增 `Messages []Message` 字段
- 新增 `ConvertAgentMessagesToLLMMessages` 辅助函数

#### Step 2: `OnSwitchChat` 加载消息到缓存
- 文件: `internal/agent/on_chat.go`
- 在加载消息后，存入 `CurrentChat.Messages`

#### Step 3: `OnNewChat` 清空缓存
- 文件: `internal/agent/on_chat_new.go`
- 重置 `CurrentChat` 时自动清空 `Messages`（零值行为）

#### Step 4: `appendNewRequestMessage` 使用缓存
- 文件: `internal/agent/on_msg_new.go`
- 用缓存最后消息的 ID 取代 DB 查询
- 写入 DB 后追加到缓存（**仅追加用户消息**，assistant 消息在 Step 6 处理）

#### Step 5: `prepareMessageForLLM` 返回缓存拷贝
- 文件: `internal/agent/on_msg_new.go`
- 在锁内拷贝 `CurrentChat.Messages`
- 新增返回值 `cachedMsgs []Message`

#### Step 6: `OnNewMessage` 使用缓存 + 流式返回后更新缓存
- 文件: `internal/agent/on_msg_new.go`
- 将 `cachedMsgs` 转为 `[]llm.Message`
- 移除 `LoadMessagesAsLLMMessages` 调用
- LLM 返回后更新缓存（带 chat 切换检测）

#### Step 7: `Manager.DeleteMessage` 同步删除缓存
- 文件: `internal/session/manager.go`
- DB 删除后从 `CurrentChat.Messages` 中移除匹配 ID 的消息

### 10. 回滚方案

如果实施后出现问题，最简单的回滚方式：
1. 还原 `llmtypes.Chat` 的 `Messages` 字段
2. `OnNewMessage` 恢复使用 `LoadMessagesAsLLMMessages`
3. 其他改动的还原
