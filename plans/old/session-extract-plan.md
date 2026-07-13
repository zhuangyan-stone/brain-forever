# Session 独立包提取计划

## 目标

将 `internal/agent/` 中的 session 相关代码提取到独立的 `internal/session/` 包，降低 `agent` 包耦合度。

## 当前状态

session 相关代码分散在 3 个文件中：

| 文件 | 内容 | 函数/类型 |
|------|------|-----------|
| `internal/agent/session.go` | session 结构体、方法 | `session`, `sessionUser`, `getSessionID`, `refreshSession`, `resolveSessionID` |
| `internal/agent/session_mgr.go` | SessionManager | `SessionManager`, `SetRedisStore`, `Close`, `NewSessionManager`, `GetOrCreate`, `Remove`, `DeleteMessage` |
| `internal/agent/chat_def.go` | chat 结构体、TitleState | `chat`, `TitleState` |

## 方案

### 目标包结构

```
internal/session/
├── session.go       ← Session, SessionUser, Chat, TitleState + 所有方法
├── manager.go       ← Manager（引用 cache.RedisSessionStore）
```

### 哪些移入 session 包

| 符号 | 移入？ | 原因 |
|------|--------|------|
| `session` struct | ✅ | 核心类型 |
| `sessionUser` struct | ✅ | 核心类型 |
| `chat` struct | ✅ | session 内部类型 |
| `TitleState` type + consts | ✅ | session 相关 |
| `session.mu`, `lastActivity`, `id`, `user` | ✅ | 字段 |
| `GetTitle()`, `SetTitle()` | ✅ | session 方法 |
| `switchToUser()`, `switchToChat()` | ✅ | session 方法 |
| `findChatBySN()`, `isBlankChat()` | ✅ | session 方法 |
| `IsAnonymous()`, `addChatToList()` | ✅ | session 方法 |
| `syncCurrentChatTitleToChatList()` | ✅ | session 方法 |
| `sessionAutoIncID` | ✅ | session 内部 |
| `generateSessionID()` | ✅ | session 内部 |
| `SessionManager` struct + methods | ✅ | session 管理器 |
| `NewSessionManager()` | ✅ | 构造函数 |
| `SetRedisStore()`, `Close()` | ✅ | 生命周期 |
| `GetOrCreate()`, `Remove()`, `DeleteMessage()` | ✅ | 核心操作 |

### 哪些留在 agent 包

| 符号 | 留在 agent？ | 原因 |
|------|-------------|------|
| `getSessionID()` | ✅ | 是 `ChatAgent` 方法，依赖 `h.cookieName` 和 `http` |
| `refreshSession()` | ✅ | 是 `ChatAgent` 方法 |
| `resolveSessionID()` | ✅ | 是 `ChatAgent` 方法 |

### 受影响文件（需更新引用）

整个 `internal/agent/` 包约 **20 个文件**需要更新引用，典型模式：

```
# 旧：同一包内直接使用
session := h.sessionManager.GetOrCreate(sessionID)
session.switchToUser(...)
session.IsAnonymous()

# 新：通过 session 包引用（如果类型是 session.Session / session.Manager）
// 取决于最终命名设计，见下方讨论
```

## 关键设计决策

### 命名问题 ⚠️

包名 `session` 与结构体名 `session` 冲突，需要解决：

**选项 A：导出时重命名**
```go
package session
type Session struct { ... }       // 原 session
type Manager struct { ... }       // 原 SessionManager
```

调用方：`session.Session{...}`、`session.Manager.GetOrCreate(...)`

**选项 B：保留原命名，调用方不使用 stutter**
这是 Go 的常见问题。如果包名是 `sess` 或 `sessn` 则不自然。

**推荐：选项 A**，将 `session` → `Session`，`SessionManager` → `Manager`。

调用方代码变化：
```go
// 旧 (agent 包内)
session := h.sessionManager.GetOrCreate(sessionID)
session.switchToUser(...)

// 新 (agent 包内，通过 session 包)
sess := h.sessionManager.GetOrCreate(sessionID)  // h.sessionManager 是 *session.Manager
sess.SwitchToUser(...)                            // 方法也需要导出
```

### 方法导出问题

当前 session 的方法都是**小写**（包内私有），提取到独立包后必须**大写**导出：

| 原方法 | 新方法 |
|--------|--------|
| `switchToUser()` | `SwitchToUser()` |
| `switchToChat()` | `SwitchToChat()` |
| `findChatBySN()` | `FindChatBySN()` |
| `isBlankChat()` | `IsBlankChat()` |
| `IsAnonymous()` | 不变（已导出） |
| `addChatToList()` | `AddChatToList()` |
| `syncCurrentChatTitleToChatList()` | `SyncCurrentChatTitleToChatList()` |

### ChatAgent 字段变化

```go
// 旧
type ChatAgent struct {
    sessionManager *SessionManager  // 同一包内
    ...
}

// 新
type ChatAgent struct {
    sessionManager *session.Manager  // 跨包引用
    ...
}
```

## 详细步骤

### 步骤 1：创建 session 包

创建 `internal/session/` 目录，包含：

**`internal/session/session.go`** — 核心类型
- `Session` struct（原 `session`）
- `SessionUser` struct（原 `sessionUser`，若需要导出）
- `Chat` struct（原 `chat`）
- `TitleState` type + consts
- 所有方法（已导出）

**`internal/session/manager.go`** — SessionManager
- `Manager` struct（原 `SessionManager`）
- 所有方法

### 步骤 2：更新 agent 包引用

需要更新的文件清单：

| 文件 | 需改动 |
|------|--------|
| `auth.go` | `session.IsAnonymous()` → `sess.IsAnonymous()` |
| `chatllm.go` | `session *session` → `session *session.Session` |
| `db.go` | `session *session` → `session *session.Session` |
| `on_chat.go` | `*SessionManager` → `*session.Manager`, `NewSessionManager()` → `session.NewManager()` |
| `on_chat_list.go` | `session.mu` → `sess.Mu` 等 |
| `on_chat_new.go` | `&chat{}` → `&session.Chat{}` |
| `on_login.go` | `session.switchToUser()` → `sess.SwitchToUser()` |
| `on_logout.go` | 同上 |
| `on_msg_new.go` | `session *session` → `session *session.Session` |
| `on_portrait.go` | `session *session` → `session *session.Session` |
| `on_portrait_title.go` | 同上 |
| `on_session.go` | `session.user.SN` → `sess.User.SN` |
| `on_title.go` | 多处引用 |
| `on_traits.go` | `session *session` → `session *session.Session`, `session.findChatBySN()` → `sess.FindChatBySN()` |
| `on_tag.go` | `session.user.chatsMu` → `sess.User.ChatsMu` |
| `on_favorites.go` | 同上 |
| `on_chat.go` | 多处引用 |
| `types.go` | `loadMessagesAsLLMMessages(s *session` → `s *session.Session` |
| `init.go` | `NewSessionManager()` → `session.NewManager()` |

### 步骤 3：重命名变量避免 stutter

```go
// 旧
session := h.sessionManager.GetOrCreate(sessionID)

// 新（建议）
sess := h.sessionManager.GetOrCreate(sessionID)
```

所有文件中的 `session` 局部变量需改为 `sess`，避免与包名 `session` 冲突。

### 步骤 4：构建并修复编译错误

预计会有一批编译错误，逐一修复即可。

## 影响范围

- 新增：`internal/session/session.go`、`internal/session/manager.go`
- 修改：`internal/agent/` 下约 20 个文件
- 删除：`internal/agent/session.go`、`internal/agent/session_mgr.go`、`internal/agent/chat_def.go`

## 风险

1. **变量命名冲突** — 局部变量 `session` 与包名冲突，需全部改为 `sess`
2. **锁方法导出** — `.mu` 字段在独立包中需要决定是否导出（建议通过方法访问，不直接暴露 `Mu`）
3. **测试** — 如果存在 session 相关的测试，需要更新导入路径
