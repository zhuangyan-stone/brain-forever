# 数据库连接生命周期重构方案

## 一、现状分析

### 1.1 当前的数据库架构

本项目使用 **SQLite (WAL模式)** 作为存储引擎，每个用户拥有独立的数据库文件：

| 数据库文件 | 用途 | 当前生命周期 |
|-----------|------|-------------|
| `localdb/users.db` | 用户注册信息 + 角色 | 进程级常驻（启动打开，关闭时关闭） |
| `localdb/anonymous.chats.db` | 匿名用户对话数据 | 进程级常驻（`InitAgent` 时创建，所有匿名 session 共享） |
| `localdb/{userNo}.chats.db` | 登录用户对话数据 | 每个 session 独立持有，打开后不关闭 |
| `localdb/anonymous.brain.db` | 匿名用户特征向量库 | 每个 session 独立持有（`GetOrCreate` 时创建） |
| `localdb/{userNo}.brain.db` | 登录用户特征向量库 | 每个 session 独立持有（`switchToUser` 时创建） |

### 1.2 当前连接管理方式

```
main.go:
  userStore = NewUserStore("localdb/users.db")   // 常驻
  defer userStore.Close()

InitAgent():
  anonymousStore = CreateLocalChatScheme("localdb/anonymous.chats.db")  // 常驻，被所有匿名 session 共享

SessionManager.GetOrCreate():
  session.chatsStore = sm.anonymousStore          // 匿名用户：共享同一个指针
  session.traitsStore = NewVectorStore("localdb/anonymous.brain.db")  // 每个 session 自建

session.switchToUser():
  旧: 关闭 oldTraitsStore
  新: chatStore = CreateLocalChatScheme(dbFile)   // 打开用户专属 DB
      session.chatsStore = chatStore               // 替换为专属 store
      session.traitsStore = NewVectorStore(...)    // 打开用户专属 vector store
```

### 1.3 关键问题

#### 问题1：匿名用户共享同一 `anonymousStore`

`anonymous.chats.db` 只对应一个 `*sqlx.DB` 实例，被所有匿名 session **共享**。虽然 `sqlx.DB` 是并发安全的，但这意味着：
- 所有匿名用户的聊天数据混杂在同一个 SQLite 文件中
- 无法区分数据属于哪个匿名 session（没有 `userNo` 字段）

#### 问题2：长连接持有文件句柄

每个 session 一旦创建了 `chatsStore` 和 `traitsStore`，就会**永久持有** `*sqlx.DB` 连接池。即使：
- 用户已经切换到其他用户（`switchToUser` 时只关闭 traitsStore，旧 chatsStore 不关闭）
- 用户长时间闲置
- session 尚未被 GC 回收

#### 问题3：`switchToUser` 不完全清理

```go
// 第 229-231 行
if oldUserNo != "" && s.chatsStore != nil {
    s.chatsStore.Close()  // 仅当 oldUserNo != "" 时关闭旧 chatsStore
}
```

如果一个匿名用户切换为登录用户（`oldUserNo == ""`），匿名 chatsStore **不会关闭**。但匿名 chatsStore 是 `SM.anonymousStore` 的共享引用，关闭它会影响到其他匿名用户。

#### 问题4：`RoleStore` 和 `UserStore` 重复打开 `users.db`

```go
// main.go
userStore, _ := store.NewUserStore("./localdb/users.db")

// 但同时 roles.go 中：
func NewRoleStore(dbPath string) (*RoleStore, error) {
    db, _ := sqlx.Open("sqlite3", dbPath+"?...")
    // 注意：这里需要 users.db 中有 roles 表
}
```

当前代码中 `RoleStore` 虽然存在但似乎未被 `main.go` 使用；不过 `users.db` 同时被 `UserStore` 操作，同一文件的多个连接池会导致 WAL 锁定问题。

---

## 二、方案设计

### 2.1 核心思路：按需打开，用完即关

将当前的"session 生命周期内常驻打开"模式，改为"每次操作时打开，操作完成后关闭"模式。

### 2.2 关键变化

#### 变化 A：`ChatStore` 不再作为 session 的长驻字段

```go
// 之前
type session struct {
    chatsStore  *store.ChatStore   // 常驻
    traitsStore *store.VectorStore // 常驻
}

// 之后
type session struct {
    userNo string  // 唯一标识，用于构建文件路径
    // 不再直接持有 *store.ChatStore 和 *store.VectorStore
}
```

#### 变化 B：引入 `DBOpener` 辅助类型

```go
// 负责打开/关闭操作，管理连接池生命周期
type DBOpener struct {
    dbDir       string   // 数据库文件目录（如 "localdb"）
    embedderDim int
    logger      zylog.Logger
}
```

提供方法：
- `OpenChatStore(userNo string) (*store.ChatStore, error)` — 打开用户聊天 DB
- `OpenVectorStore(userNo string) (*store.VectorStore, error)` — 打开用户特征 DB
- `CloseChatStore(s *store.ChatStore)` — 立即关闭
- `CloseVectorStore(s *store.VectorStore)` — 立即关闭

每个操作都遵循 **open → 使用 → close** 的模式。

#### 变化 C：匿名用户也使用独立 DB

```go
// 匿名用户：使用 sessionID 的 hash 作为"伪 userNo"
func anonymousDBName(sessionID string) string {
    hash := md5.Sum([]byte(sessionID))
    return fmt.Sprintf("anon_%x", hash[:8])  // 如 "anon_a1b2c3d4"
}
```

这样每个匿名 session 拥有独立的 `.chats.db` 和 `.brain.db`，不再共享 `anonymousStore`。

#### 变化 D：`UserStore` 和 `RoleStore` 共享同一连接

合并 `UserStore` 和 `RoleStore` 的操作到同一个 `*sqlx.DB` 实例，避免同一文件的多个连接池。

### 2.3 核心流程对比

#### 发送消息（`OnNewMessage`）

```
之前：
  1. 从 session.chatsStore 直接查询 ← 句柄已打开
  2. 写消息到 DB
  3. 返回

之后：
  1. dbOpener.OpenChatStore(session.userNo)  ← 按需打开
  2. （使用 chatStore 查询/写入）
  3. dbOpener.CloseChatStore(chatStore)       ← 用完即关
  4. 返回
```

#### 切换用户（`switchToUser`）

```
之前：
  1. 关闭旧 traitsStore（条件性）
  2. 打开新 chatStore → 赋值给 session.chatsStore（永久持有）
  3. 打开新 traitsStore → 赋值给 session.traitsStore（永久持有）

之后：
  1. 更新 session.userNo（仅记录标识符）
  2. 不再打开任何数据库连接
```

### 2.4 性能考量

**Q: 每次操作都打开/关闭 SQLite，性能会下降吗？**

A: 不会显著下降，因为：
1. `sqlx.Open()` 是**懒加载**的，不会立即打开文件句柄，只初始化一个结构体
2. 实际的 SQLite 文件打开发生在**第一次查询时**
3. SQLite 在 WAL 模式下，打开/关闭代价很小
4. `CREATE TABLE IF NOT EXISTS` 等 DDL 可以**按需执行**（只在首次打开时检查）

对于高频操作（如 SSE streaming 中需要多次写入），可以在 streaming 开始前打开，结束后关闭（单次 streaming 期间保持连接）。

### 2.5 需要迁移的 Handler

所有通过 `session.chatsStore` 和 `session.traitsStore` 访问数据库的地方都需要修改：

| Handler 方法 | 使用的 Store | 操作频率 |
|-------------|-------------|---------|
| `OnNewMessage` | chatsStore | 高频（每次用户消息） |
| `OnSwitchChat` | chatsStore | 中频 |
| `OnGetChats` | chatsStore | 中频 |
| `OnChatDelete` | chatsStore | 低频 |
| `OnRestoreChat` | chatsStore | 低频 |
| `OnPermanentDelete` | chatsStore + traitsStore | 低频 |
| `OnListDeletedChats` | chatsStore | 低频 |
| `OnEmptyTrash` | chatsStore | 低频 |
| `OnPutChatTitle` | chatsStore | 低频 |
| `OnGetSuggestedChatTitle` | chatsStore | 低频 |
| `OnNewChat` | chatsStore | 中频 |
| `OnDeleteMessage` | chatsStore | 低频 |
| `OnChatPin` | chatsStore | 低频 |
| `OnExtractTraits` | chatsStore + traitsStore | 低频 |
| `OnGetUserPortrait` | traitsStore | 低频 |
| `OnSession` | chatsStore | 中频（每次页面加载） |
| `OnGetLLMInfo` | - | 不需要 |
| `OnLogin`/`OnLogout` | - | 低频 |
| `OnChatGroups` | chatsStore | 中频 |
| `ListFavoriteChats`/`AddFavoriteChat`/`RemoveFavoriteChat` | chatsStore | 低频 |
| `OnGenerateChatTags` | chatsStore | 低频 |
| `OnGetDocTitle` | traitsStore | 低频 |

---

## 三、实施步骤

### Step 1：重构 `ChatStore` 构造函数 — 分离 Schema 初始化

**目标**：将 schema 初始化与数据库打开分离，避免每次打开都运行 DDL。

**变更文件**：[`internal/store/chats.go`](internal/store/chats.go)

- 新增 `OpenChatStore(dbFile string) (*ChatStore, error)` — 仅打开数据库，不执行 DDL
- 新增 `EnsureSchema() error` — 独立执行 DDL 的方法
- 保留 `CreateLocalChatScheme()` 作为"打开+初始化"的便捷方法（用于首次创建）
- 添加 `IsOpen() bool` 辅助方法

```go
// OpenChatStore 打开一个已有的聊天数据库，不执行 DDL
func OpenChatStore(dbFile string) (*ChatStore, error) {
    db, err := sqlx.Open("sqlite3", dbFile+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1")
    if err != nil {
        return nil, err
    }
    return &ChatStore{db: db}, nil
}

// EnsureSchema 确保数据库表结构存在（幂等）
func (s *ChatStore) EnsureSchema() error {
    return s.initSchema()
}
```

### Step 2：创建 `DBOpener` 管理器

**新增文件**：[`internal/store/db_opener.go`](internal/store/db_opener.go)

```go
type DBOpener struct {
    dbDir       string         // 数据库目录，默认 "localdb"
    embedderDim int
    logger      zylog.Logger
}

func NewDBOpener(dbDir string, embedderDim int, logger zylog.Logger) *DBOpener

// ChatStore 管理
func (o *DBOpener) OpenChatStore(userNo string) (*ChatStore, error)
func (o *DBOpener) CloseChatStore(s *ChatStore)

// VectorStore 管理
func (o *DBOpener) OpenVectorStore(userNo string) (*VectorStore, error)
func (o *DBOpener) CloseVectorStore(s *VectorStore)
```

- `OpenChatStore("")` → 匿名用户，使用 `localdb/anon_{hash}.chats.db`
- `OpenChatStore("user_xxx")` → 登录用户，使用 `localdb/user_xxx.chats.db`
- `OpenChatStore` 内部调用 `OpenChatStore(dbFile)` 然后 `EnsureSchema()`

### Step 3：重构 `session` 结构体

**变更文件**：[`internal/agent/types.go`](internal/agent/types.go)

- 从 `session` 中移除 `chatsStore *store.ChatStore` 和 `traitsStore *store.VectorStore`
- 保留 `userNo string` 作为标识
- 修改 `SessionManager`：移除 `anonymousStore`，改为持有 `*DBOpener`
- 修改 `GetOrCreate`：不再创建 VectorStore

```go
type session struct {
    mu      sync.Mutex
    chatsMu sync.Mutex

    lastActivity time.Time
    id          string
    currentChat *chat
    chats       []store.Chat
    userNo      string
    // chatsStore 和 traitsStore 已移除

    embedderDim int
    logger      zylog.Logger
}

type SessionManager struct {
    mu       sync.RWMutex
    sessions map[string]*session
    dbOpener *store.DBOpener    // 替代 anonymousStore
    logger   zylog.Logger
}
```

### Step 4：修改 `switchToUser` 和 `GetOrCreate`

**变更文件**：[`internal/agent/types.go`](internal/agent/types.go)

- `switchToUser`：仅更新 `userNo`，不再打开/关闭任何数据库
- `GetOrCreate`：仅创建 session，不再创建 VectorStore

### Step 5：重构所有 Handler 方法

**变更文件**：[`internal/agent/*.go`](internal/agent/*.go)

每个 handler 方法的模式改为：

```go
func (h *ChatAgent) OnXxx(w http.ResponseWriter, r *http.Request) {
    // 1. 获取 session
    session := h.sm.GetOrCreate(sessionID)
    
    // 2. 按需打开数据库
    chatStore, err := h.sm.dbOpener.OpenChatStore(session.userNo)
    if err != nil { ... }
    defer h.sm.dbOpener.CloseChatStore(chatStore)
    
    // 3. 使用 chatStore 执行操作
    // ...
}
```

对于需要同时使用 `chatsStore` 和 `traitsStore` 的方法，两者都打开：

```go
chatStore, err := h.sm.dbOpener.OpenChatStore(session.userNo)
defer h.sm.dbOpener.CloseChatStore(chatStore)

traitStore, err := h.sm.dbOpener.OpenVectorStore(session.userNo)
defer h.sm.dbOpener.CloseVectorStore(traitStore)
```

**特殊处理：SSE Streaming（`OnNewMessage`）**

在 streaming 过程中，需要多次写消息到数据库。为避免频繁打开/关闭，**在 streaming 开始前打开，streaming 结束后关闭**：

```go
func (h *ChatAgent) OnNewMessage(w http.ResponseWriter, r *http.Request) {
    session := h.sm.GetOrCreate(sessionID)
    
    chatStore, err := h.sm.dbOpener.OpenChatStore(session.userNo)
    if err != nil { ... }
    defer h.sm.dbOpener.CloseChatStore(chatStore)  // streaming 结束后关闭

    // ... 在 streaming 循环中直接使用 chatStore ...
}
```

### Step 6：清理 `SessionManager.Close()`

**变更文件**：[`internal/agent/types.go`](internal/agent/types.go)

不再需要关闭所有 per-session 的 store，删除相关逻辑。

```go
func (sm *SessionManager) Close() {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    // 仅清理 session map
    sm.sessions = make(map[string]*session)
    // dbOpener 不需要 Close（它不持有长连接）
}
```

### Step 7：合并 `UserStore` 和 `RoleStore`（可选优化）

**变更文件**：[`internal/store/users.go`](internal/store/users.go), [`internal/store/roles.go`]

- 将 `roles` 表的 schema 初始化合并到 `UserStore.initSchema()` 中
- `NewUserStore` 创建 `users` + `roles` 两张表
- 移除 `NewRoleStore` 或将其改为接收 `*sqlx.DB` 参数

---

## 四、实施路线图

```
Phase 1: 基础设施重构
  ├── Step 1: 重构 ChatStore 构造函数（OpenChatStore + EnsureSchema）
  ├── Step 2: 创建 DBOpener 管理器
  ├── Step 3: 修改 Session/DBOpener
  └── Step 4: UserStore+RoleStore 合并（可选）

Phase 2: Handler 迁移
  ├── Step 5: 迁移只读操作（ListChats, GetChats, SwitchChat 等）
  ├── Step 6: 迁移写入操作（NewMessage, Delete 等）
  └── Step 7: 特殊处理 SSE Streaming

Phase 3: 清理与测试
  ├── Step 8: 清理 SessionManager 中长连接逻辑
  ├── Step 9: 移除 anonymousStore
  ├── Step 10: 测试（单元测试 + 集成测试）
  └── Step 11: 性能基准对比
```

---

## 五、风险与注意事项

### 风险1：匿名用户数据隔离

当前匿名用户共享 `anonymous.chats.db`。改为每个 session 独立 DB 后，**旧数据库中已有的匿名数据需要迁移**。

**建议**：新旧数据分离，旧数据保留在 `anonymous.chats.db` 中只读访问，新数据写入独立文件。或在首次切换时一次性迁移。

### 风险2：SSE Streaming 期间的连接状态

Streaming 过程中要保持连接打开。如果在 streaming 中途连接断开（panic、网络中断），需要确保资源正确关闭。

**建议**：在 streaming handler 中使用 `defer` 确保关闭。

### 风险3：并发打开同一用户的数据库

如果同一用户同时打开多个浏览器 Tab，每个 Tab 对应不同的 HTTP session，它们会尝试同时打开同一个 `.chats.db` 文件。SQLite WAL 模式支持多个读者/一个写者，这通常没问题。但如果有多个 session 同时写入，可能会遇到 `database is locked` 错误。

**建议**：保持 `chatsMu` 等现有锁机制，或引入一个**全局的 per-user 锁**。

### 风险4：性能退化

对于高频且简单的查询（如每次 streaming token 写入），打开/关闭连接的开销可能累积。

**建议**：实施后监控打开/关闭耗时，必要时为高频操作引入短生命周期缓存。

---

## 六、决策点

需要你确认以下几个关键决策：

1. **匿名用户是否也需要独立数据库？**
   - 方案A：每个匿名 session 持有独立 `.chats.db`（数据隔离好，但文件多）
   - 方案B：匿名用户继续共享 `anonymous.chats.db`（简单，但无隔离）

2. **`UserStore` 是否需要常驻？**
   - 方案A：也改为按需打开（一致性高）
   - 方案B：保持常驻（`users.db` 是全局公用，打开频率高）

3. **是否需要引入 per-user 连接池缓存？**
   - 方案A：纯按需打开/关闭（最简洁）
   - 方案B：LRU 缓存，缓存最近 N 个用户的连接（性能更好）
