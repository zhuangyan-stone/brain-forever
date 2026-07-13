# 方向 A：session.chats 独立锁方案

## 问题

流式回复期间（`OnNewMessage` 持有 `session.mu` 10-60 秒），
侧栏操作（重命名、Pin、删除其他会话）虽已采用窄锁设计，但入口处的
`session.mu.Lock()` 仍被阻塞，且这些操作与 `currentChat.history` 无任何数据冲突。

## 方案

给 `session.chats` 和 `session.chatStore` 分配独立锁 `chatsMu`，
使侧栏操作完全绕过 `session.mu`，不再受流式回复阻塞。

## 涉及的文件和变更

### 1. [`internal/agent/types.go`](internal/agent/types.go)

#### 1a. session 结构体新增 `chatsMu`

```go
type session struct {
    mu           sync.Mutex       // 保护：currentChat, userNo, lastActivity
    chatsMu      sync.Mutex       // 新增：保护 chats, chatStore
    lastActivity time.Time
    currentChat  *chat
    chats        []store.Session
    userNo       string
    chatStore    *store.ChatStore
}
```

#### 1b. switchToUser 拆分锁

当前 `switchToUser` 在 `session.mu` 锁内做 IO（创建 DB、查询会话列表），
可将 IO 移出锁外，再分别获取两把锁更新对应字段。

```go
func (s *session) switchToUser(sn string) {
    // 阶段 1：无锁 IO
    dbFile := "data/" + sn + ".chats.db"
    chatStore, err := store.CreateLocalChatScheme(dbFile)
    if err != nil {
        log.Printf("failed to create local chat scheme for user %s: %v", sn, err)
        return
    }
    chats, err := chatStore.ListSessions(100)
    if err != nil {
        log.Printf("failed to list sessions for user %s: %v", sn, err)
        return
    }

    // 阶段 2：chatsMu 保护 chats + chatStore
    s.chatsMu.Lock()
    s.chatStore = chatStore
    s.chats = chats
    s.chatsMu.Unlock()

    // 阶段 3：mu 保护 currentChat + userNo
    s.mu.Lock()
    s.currentChat = nil
    s.userNo = sn
    s.mu.Unlock()
}
```

**注意**：`OnLogin` 在 `switchToUser` 返回后读取 `session.chats`（line 49），
需在 `chatsMu` 保护下读取。

### 2. [`internal/agent/on_session.go`](internal/agent/on_session.go)

#### 2a. OnPutSessionTitle（sn != ""，侧栏重命名）

将 `session.mu.Lock()` 替换为 `session.chatsMu.Lock()`。
`session.chatStore` 也在此锁下读取（与 `chatsMu` 同锁保护）。

```go
// 当前（简化）：
//   session.mu.Lock()
//   → 在 session.chats 中查找目标
//   → 读 session.chatStore
//   session.mu.Unlock()
//   → DB 写
//   session.mu.Lock()
//   → 更新 session.chats[i]
//   session.mu.Unlock()

// 改为：
session.chatsMu.Lock()
// 在 session.chats 中查找目标
// 读 session.chatStore
session.chatsMu.Unlock()

// DB 写（chatStore 方法自身线程安全）

session.chatsMu.Lock()
// 更新 session.chats[i]
session.chatsMu.Unlock()
```

#### 2b. OnUpdateSessionPin（侧栏 Pin 切换）

同理，将 `session.mu.Lock() / defer session.mu.Unlock()` 替换为 `session.chatsMu`。

```go
session.chatsMu.Lock()
defer session.chatsMu.Unlock()

// 在 session.chats 中查找目标
// DB 写
// 更新 targetSession.Pinned
```

#### 2c. OnDeleteSession（侧栏删除会话）

同理。

```go
session.chatsMu.Lock()
defer session.chatsMu.Unlock()

// 在 session.chats 中查找
// DB 写
// 替换 session.chats = filtered
```

### 3. [`internal/agent/on_login.go`](internal/agent/on_login.go)

`OnLogin` 在 `switchToUser` 后读取 `session.chats` 返回给前端：

```go
// 当前：
// json.NewEncoder(w).Encode(map[string]interface{}{
//     "sessions": session.chats,  // 无锁读取
// })

// 改为：
session.chatsMu.Lock()
sessions := session.chats
session.chatsMu.Unlock()

json.NewEncoder(w).Encode(map[string]interface{}{
    "sessions": sessions,
})
```

## 锁层级变更对照

| 锁 | 保护范围 | 持有者（变更前） | 持有者（变更后） |
|---|---------|----------------|----------------|
| `session.mu` | `currentChat`, `userNo`, `lastActivity` | 所有 handler | 仅 `OnNewMessage`, `OnRestoreSession`, `OnGetSessionTitle`, `OnPutSessionTitle(sn=="")`, `OnDeleteMessage`, `switchToUser`（部分） |
| `session.chatsMu` | `chats`, `chatStore` | 不存在 | `OnPutSessionTitle(sn!="")`, `OnUpdateSessionPin`, `OnDeleteSession`, `switchToUser`（部分）, `OnLogin` |

## 收益

| 操作 | 改进前 | 改进后 |
|------|-------|-------|
| 流式中侧栏重命名 | 阻塞 10-60 秒 | **微秒级**立即执行 |
| 流式中侧栏 Pin | 阻塞 10-60 秒 | **微秒级**立即执行 |
| 流式中侧栏删除会话 | 阻塞 10-60 秒 | **微秒级**立即执行 |
| 流式中登录 | 阻塞 10-60 秒 | **微秒级**立即执行 |
| 流式本身 | 不变 | 不变（仍持 `session.mu`） |

## 安全分析

1. **无死锁风险**：`chatsMu` 和 `session.mu` 永远不会嵌套获取。
   各 handler 只获取其中一把锁，不存在 ABBA 场景。
2. **`chatStore` 安全性**：`chatStore` 在 `switchToUser` 中设置，此后不变。
   所有侧栏操作均需用户已登录（`chatStore != nil`），因此 `switchToUser` 已执行完毕。
3. **DB 线程安全**：`chatStore` 的 SQLite 方法自身支持并发（WAL 模式），
   多个 goroutine 同时调用不同的 `chatStore` 方法（如 InsertMessage + UpdateSessionTitle）是安全的。
