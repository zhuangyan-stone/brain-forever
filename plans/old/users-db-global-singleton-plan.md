# 数据库架构重构执行计划

> 创建日期: 2026-07-05
> 状态: 待实现
> 范围: **仅后端**

---

## 一、总览

本次变更涉及四个核心目标：

1. **`theUserStore` 全局单例** — `users.db` 在程序启动时打开，退出时关闭
2. **`dbcfg` 子包** — 替代 `DBOpener`，包级全局函数模式，移除所有依赖注入
3. **`VectorStore` → `BrainStore`** 类型重命名
4. **`UserStore.Login/Logout` 方法** — 在 `users.go` 中新增
5. **取消匿名用户 + `isRealUser` 全删除** — `AnonymousDBName` 移除，`userNo` → `userSN`，前端决定 SN

---

## 二、关键设计决策

### 2.1 `dbcfg` 包（`store/dbcfg/`）

**包级全局模式**，类似 [`theLogger`](internal/logger/the_logger.go)：

- 不导出结构体，只有包级函数
- `InitDBConfig(dbDir, embedderDim, logger)` — 程序启动时初始化
- `InitLocalChatDB(userSN)` / `OpenLocalChatDB(userSN)` / `CloseLocalChatDB(s)` — chats.db
- `InitLocalBrainDB(userSN)` / `OpenLocalBrainDB(userSN)` / `CloseLocalBrainDB(s)` — brain.db
- `dbPath(userSN, kind)` + `ensureDir()` — 内部辅助方法

**包级变量**：
```go
var (
    theDBDir       string
    theEmbedderDim int
    theLogger      zylog.Logger
)
```

**调用示例**：
```go
// 任意 handler 中：
chatStore, err := dbcfg.OpenLocalChatDB(userSN)
defer dbcfg.CloseLocalChatDB(chatStore)
```

### 2.2 移除所有依赖注入

| 旧模式 | 新模式 |
|-------|--------|
| `SessionManager.dbOpener *store.DBOpener` | **移除** — 全局 `dbcfg` |
| `SessionManager.embedderDim` | **移除** — 全局 `dbcfg` |
| `SessionManager.logger` | **移除** — 全局 `dbcfg` |
| `session.embedderDim` | **移除** |
| `session.logger` | **移除** |
| `ChatAgent.isRealUser` | **移除** — 全局 `dbcfg.IsRealUser` |
| `NewChatHandler(localDB)` | **移除** 该参数 |
| `switchToUser(sn, localDB)` | **简化** 为 `switchToUser(sn)` |

### 2.3 `SessionManager` 精简后

```go
type SessionManager struct {
    mu       sync.RWMutex
    sessions map[string]*session
}

func NewSessionManager() *SessionManager
```

### 2.4 不再有匿名用户

- `AnonymousDBName` 函数删除
- `resolveUserSN` 直接返回 `session.userSN`，无 fallback
- `userSN = ""` 表示未登录，API 应拒绝服务

### 2.5 `theUserStore` 全局单例

模式同 [`theLogger`](internal/logger/the_logger.go)：
- `store.InitTheUserStore(dbDir)` — HTTP 监听前调用，同时记录 `theDBDir`
- `store.TheUserStore()` — 任意位置获取
- `store.CloseTheUserStore()` — HTTP 停止后调用

### 2.6 `UserStore.Login/Logout` 方法

作为 `UserStore` 的方法，存放在 [`store/users.go`](internal/store/users.go) 中：

```go
// UserStore manages user storage
type UserStore struct {
    db    *sqlx.DB
    dbDir string   // added: database directory for constructing chat db paths
}

// Login loads a user's chat list and ensures DB schema exists.
func (s *UserStore) Login(userSN string) ([]Chat, error) {
    dbPath := filepath.Join(s.dbDir, userSN+".chats.db")
    chatStore, err := CreateLocalChatScheme(dbPath)
    if err != nil {
        return nil, fmt.Errorf("login failed for %s: %w", userSN, err)
    }
    defer chatStore.Close()
    return chatStore.ListChats(100)
}

// Logout placeholder for future cleanup.
func (s *UserStore) Logout(userSN string) {
    _ = userSN  // Future: update last_logout timestamp, etc.
}
```

调用方式：`store.TheUserStore().Login(userSN)`

---

## 三、Step-by-Step 实施步骤

### Step 1: 新建 [`store/dbcfg/dbcfg.go`](store/dbcfg/dbcfg.go)

```go
// Package dbcfg provides global on-demand access to per-user SQLite databases.
// Uses package-level state (like theLogger pattern) to avoid dependency injection.
package dbcfg

import (
    "fmt"
    "os"
    "path/filepath"

    "BrainForever/infra/zylog"
    "BrainForever/internal/store"
)

// Package-level configuration, initialized once at startup.
var (
    theDBDir       string
    theEmbedderDim int
    theLogger      zylog.Logger
)

// InitDBConfig initializes the dbcfg package. Must be called once at startup.
func InitDBConfig(dbDir string, embedderDim int, logger zylog.Logger) {
    theDBDir = dbDir
    theEmbedderDim = embedderDim
    theLogger = logger
}

// dbPath returns: {dbDir}/{userSN}.{dbKind}.db
func dbPath(userSN string, dbKind string) string {
    return filepath.Join(theDBDir, userSN+"."+dbKind+".db")
}

// ensureDir creates the database directory if needed.
func ensureDir() error {
    return os.MkdirAll(theDBDir, 0755)
}

// InitLocalChatDB opens a user's chat database and ensures its schema exists.
// Use this when the database may not exist yet (e.g., first login/switchToUser).
// Caller MUST call CloseLocalChatDB when done.
func InitLocalChatDB(userSN string) (*store.ChatStore, error) {
    if err := ensureDir(); err != nil {
        return nil, fmt.Errorf("failed to create db dir %s: %w", theDBDir, err)
    }
    s, err := store.CreateLocalChatScheme(dbPath(userSN, "chats"))
    if err != nil {
        return nil, fmt.Errorf("failed to init chat db for %s: %w", userSN, err)
    }
    return s, nil
}

// OpenLocalChatDB opens a user's chat database WITHOUT schema initialization.
// Faster than InitLocalChatDB; used on hot paths where schema already exists.
// Caller MUST call CloseLocalChatDB when done.
func OpenLocalChatDB(userSN string) (*store.ChatStore, error) {
    if err := ensureDir(); err != nil {
        return nil, fmt.Errorf("failed to create db dir %s: %w", theDBDir, err)
    }
    s, err := store.OpenChatStore(dbPath(userSN, "chats"))
    if err != nil {
        return nil, fmt.Errorf("failed to open chat db for %s: %w", userSN, err)
    }
    return s, nil
}

// CloseLocalChatDB closes a chat database. Nil-safe.
func CloseLocalChatDB(s *store.ChatStore) {
    if s != nil {
        s.Close()
    }
}

// InitLocalBrainDB opens a user's brain database and ensures its schema exists.
// Use this when the database may not exist yet (e.g., first login).
// Caller MUST call CloseLocalBrainDB when done.
func InitLocalBrainDB(userSN string) (*store.BrainStore, error) {
    if err := ensureDir(); err != nil {
        return nil, fmt.Errorf("failed to create db dir %s: %w", theDBDir, err)
    }
    s, err := store.NewBrainStore(dbPath(userSN, "brain"), theEmbedderDim, theLogger)
    if err != nil {
        return nil, fmt.Errorf("failed to init brain db for %s: %w", userSN, err)
    }
    return s, nil
}

// OpenLocalBrainDB opens a user's brain database WITHOUT schema initialization.
// Faster than InitLocalBrainDB; used on hot paths where schema already exists.
// Caller MUST call CloseLocalBrainDB when done.
func OpenLocalBrainDB(userSN string) (*store.BrainStore, error) {
    if err := ensureDir(); err != nil {
        return nil, fmt.Errorf("failed to create db dir %s: %w", theDBDir, err)
    }
    s, err := store.OpenBrainStore(dbPath(userSN, "brain"), theEmbedderDim, theLogger)
    if err != nil {
        return nil, fmt.Errorf("failed to open brain db for %s: %w", userSN, err)
    }
    return s, nil
}

// CloseLocalBrainDB closes a brain database. Nil-safe.
func CloseLocalBrainDB(s *store.BrainStore) {
    if s != nil {
        s.Close()
    }
}
```

### Step 2: 修改 [`store/users.go`](internal/store/users.go)

2a. `UserStore` 结构体增加 `dbDir` 字段：
```go
type UserStore struct {
    db    *sqlx.DB
    dbDir string   // 数据库目录，用于构造 chat db 路径
}
```

2b. 新增 `Login` / `Logout` 方法：
```go
// Login loads a user's chat list and ensures their databases exist.
func (s *UserStore) Login(userSN string) ([]Chat, error) {
    dbPath := filepath.Join(s.dbDir, userSN+".chats.db")
    chatStore, err := CreateLocalChatScheme(dbPath)
    if err != nil {
        return nil, fmt.Errorf("login failed for %s: %w", userSN, err)
    }
    defer chatStore.Close()
    return chatStore.ListChats(100)
}

// Logout is called when a user logs out. Currently a no-op placeholder.
func (s *UserStore) Logout(userSN string) {
    _ = userSN
}
```

### Step 3: 新建 [`store/the_user_store.go`](store/the_user_store.go)

```go
var theUserStore *UserStore

func TheUserStore() *UserStore {
    if theUserStore == nil {
        panic("TheUserStore is nil - call InitTheUserStore first")
    }
    return theUserStore
}

func InitTheUserStore(dbDir string) error {
    s, err := OpenUserStore(filepath.Join(dbDir, "users.db"))
    if err != nil {
        return err
    }
    s.dbDir = dbDir   // 设置目录供 Login 使用
    theUserStore = s
    return nil
}

func CloseTheUserStore() {
    if theUserStore != nil {
        theUserStore.Close()
    }
}
```

```go
package store

import "path/filepath"

var theUserStore *UserStore

func TheUserStore() *UserStore {
    if theUserStore == nil {
        panic("TheUserStore is nil - call InitTheUserStore first")
    }
    return theUserStore
}

func InitTheUserStore(dbDir string) error {
    s, err := OpenUserStore(filepath.Join(dbDir, "users.db"))
    if err != nil {
        return err
    }
    theUserStore = s
    return nil
}

func CloseTheUserStore() {
    if theUserStore != nil {
        theUserStore.Close()
    }
}
```

### Step 4: 重命名 [`store/traits.go`](store/traits.go) 中的 `VectorStore` → `BrainStore`

全局替换：

| 旧 | 新 |
|---|----|
| 类型 `VectorStore` | `BrainStore` |
| `OpenVectorStore()` | `OpenBrainStore()` |
| `NewVectorStore()` | `NewBrainStore()` |
| 所有方法接收者 `(s *VectorStore)` | `(s *BrainStore)` |
| 所有注释 | 更新 |

### Step 5: 修改 [`internal/agent/types.go`](internal/agent/types.go)

4a. `session` 结构体：
```go
type session struct {
    mu           sync.Mutex
    chatsMu      sync.Mutex
    lastActivity time.Time
    id           string
    currentChat  *chat
    chats        []store.Chat
    userSN       string    // 原 userNo
    // embedderDim, logger 已移除
}
```

4b. `SessionManager` 结构体（大幅精简）：
```go
type SessionManager struct {
    mu       sync.RWMutex
    sessions map[string]*session
}

func NewSessionManager() *SessionManager {
    return &SessionManager{
        sessions: make(map[string]*session),
    }
}
```

4c. `session.GetOrCreate` — 精简：
```go
s = &session{
    id:           sessionID,
    lastActivity: time.Now(),
    userSN:       "",
    currentChat:  &chat{},
}
```

4d. `DeleteMessage` — 使用 `dbcfg`：
```go
func (sm *SessionManager) DeleteMessage(sessionID string, msgID int64) error {
    // ... find session ...
    
    userSN := s.userSN   // 直接取，无 fallback
    
    chatStore, err := dbcfg.OpenLocalChatDB(userSN)
    if err != nil {
        return fmt.Errorf("failed to open chat store: %w", err)
    }
    defer dbcfg.CloseLocalChatDB(chatStore)
    
    return chatStore.DeleteMessageGroup(chatID, int(msgID))
}
```

4e. `session.switchToUser` — 简化为直接设置状态，chats 由调用方传入：
```go
// switchToUser sets the session's user state.
// For login: sn is non-empty, chats are pre-loaded by UserStore.Login().
// For logout: sn is empty, chats is nil.
func (s *session) switchToUser(sn string, chats []store.Chat) {
    if chats == nil {
        chats = []store.Chat{}
    }
    s.chatsMu.Lock()
    s.chats = chats
    s.chatsMu.Unlock()

    s.mu.Lock()
    s.currentChat = &chat{}
    s.userSN = sn
    s.mu.Unlock()
}
```

### Step 6: 修改 [`internal/agent/on_chat.go`](internal/agent/on_chat.go)

5a. 辅助方法 — 使用 `dbcfg`：
```go
func resolveUserSN(s *session) string {
    return s.userSN   // 原 resolveUserNo，无 fallback
}

func (h *ChatAgent) openChatDB(s *session) (*store.ChatStore, error) {
    // Use Open (no schema check) for performance on hot paths.
    // Schema is ensured by InitLocalChatDB called during login.
    return dbcfg.OpenLocalChatDB(resolveUserSN(s))
}

func (h *ChatAgent) closeChatDB(cs *store.ChatStore) {
    dbcfg.CloseLocalChatDB(cs)
}

func (h *ChatAgent) openBrainDB(s *session) (*store.BrainStore, error) {
    // Use Open (no schema check) for performance on hot paths.
    return dbcfg.OpenLocalBrainDB(resolveUserSN(s))
}

func (h *ChatAgent) closeBrainDB(vs *store.BrainStore) {
    dbcfg.CloseLocalBrainDB(vs)
}
```

5b. `ChatAgent` 结构体 — 移除 `isRealUser`：
```go
type ChatAgent struct {
    embedder       embedder.Embedder
    webSearcher    toolimp.WebSearcher
    charLLMClient  llm.Client
    sessionManager *SessionManager
    cookieName     string
    defaultLang    string
    avatarDir      string
    logger         zylog.Logger
    // isRealUser 已移除，使用 dbcfg.IsRealUser
}
```

5c. `NewChatHandler` — 精简参数：
```go
func NewChatHandler(
    embedder embedder.Embedder,
    webSearcher toolimp.WebSearcher,
    chatLLMClient llm.Client,
    cookieName string,
    defaultLang string,
    avatarDir string,
    logger zylog.Logger,
) *ChatAgent {
    // localDB 参数已移除
    // isRealUser 参数已移除
    return &ChatAgent{
        embedder:       embedder,
        webSearcher:    webSearcher,
        charLLMClient:  chatLLMClient,
        sessionManager: NewSessionManager(),
        cookieName:     cookieName,
        defaultLang:    defaultLang,
        avatarDir:      avatarDir,
        logger:         logger,
    }
}
```

5d. 全局搜索替换调用方法名：

| 旧 | 新 |
|---|----|
| `h.openChatStore(session)` | `h.openChatDB(session)` |
| `h.closeChatStore(chatStore)` | `h.closeChatDB(chatStore)` |
| `h.openTraitsStore(session)` | `h.openBrainDB(session)` |
| `h.closeTraitsStore(traitsStore)` | `h.closeBrainDB(brainStore)` |

涉及文件：
- [`internal/agent/on_chat.go`](internal/agent/on_chat.go)
- [`internal/agent/on_msg_new.go`](internal/agent/on_msg_new.go)
- [`internal/agent/on_portrait.go`](internal/agent/on_portrait.go)
- [`internal/agent/on_traits.go`](internal/agent/on_traits.go)
- [`internal/agent/on_title.go`](internal/agent/on_title.go)

### Step 6: 修改 [`internal/agent/init.go`](internal/agent/init.go)

```go
func InitAgent(ctx context.Context, cfg config.Config, cookieName string, defaultLang string, logger zylog.Logger) (*ChatAgent, error) {
    // 1. Init Embedder
    embeddingClient := InitEmbedder(cfg.Embedder, logger)

    // 2. Init Chat LLM
    chatLLMClient := InitLLMClient(cfg.ChatLLM, logger)

    // 3. Init Web Search
    webSearchClient := InitWebSearchClient(cfg.WebSearch, logger)

    // 4. Init dbcfg (replaces DBOpener)
    dbcfg.InitDBConfig("localdb", embeddingClient.Dimension(), logger)

    // 5. Avatar dir
    avatarDir := cfg.Frontend.Dir + "/static/img/avatar"

    // 6. Create ChatHandler (no more dbOpener/isRealUser params)
    chatHandler := NewChatHandler(
        embeddingClient,
        webSearchClient,
        chatLLMClient,
        cookieName,
        defaultLang,
        avatarDir,
        logger,
    )
    return chatHandler, nil
}
```

### Step 7: 修改 [`internal/agent/on_login.go`](internal/agent/on_login.go)

```go
type LoginRequest struct {
    UserSN string `json:"user_sn"`    // 前端传入：test_user_001 或 anonymous 等
}

func (h *ChatAgent) OnLogin(w http.ResponseWriter, r *http.Request) {
    var req LoginRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
        return
    }
    if req.UserSN == "" {
        http.Error(w, `{"error":"user_sn is required"}`, http.StatusBadRequest)
        return
    }

    sessionID := h.resolveSessionID(w, r)
    session := h.sessionManager.GetOrCreate(sessionID)

    // Call store.Login to load chat data
    session.switchToUser(req.UserSN)  // 内部调用 store.Login

    session.chatsMu.Lock()
    chats := session.chats
    session.chatsMu.Unlock()
    if chats == nil {
        chats = []store.Chat{}
    }

    avatar := pickRandomAvatar(h.avatarDir)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "status":  "ok",
        "user_sn": userSN,    // 原 user_no
        "avatar":  avatar,
        "chats":   chats,
    })
}
```

### Step 9: 修改 [`internal/agent/on_logout.go`](internal/agent/on_logout.go)

```go
func (h *ChatAgent) OnLogout(w http.ResponseWriter, r *http.Request) {
    sessionID := h.resolveSessionID(w, r)
    session := h.sessionManager.GetOrCreate(sessionID)
    session.switchToUser("")   // 不再传 localDB

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "status": "ok",
    })
}
```

### Step 10: 修改 [`internal/agent/on_session.go`](internal/agent/on_session.go)

```go
json.NewEncoder(w).Encode(map[string]interface{}{
    "user_sn": session.userSN,    // 原 user_no
    "welcome": welcome,
})
```

### Step 11: 更新 [`internal/agent/trait_searcher.go`](internal/agent/trait_searcher.go)

```go
// 旧:
store  *store.VectorStore

// 新:
store  *store.BrainStore
```

### Step 12: 修改 [`cmd/server/main.go`](cmd/server/main.go)

```go
func main() {
    // ... 现有初始化代码 ...

    // 确保 localdb 目录
    if err := os.MkdirAll("./localdb", 0755); err != nil {
        theLogger.Fatalf("failed to create localdb directory: %v", err)
    }

    // 初始化 theUserStore 全局单例（也设置 theDBDir 供 Login/Logout 使用）
    if err := store.InitTheUserStore("./localdb"); err != nil {
        theLogger.Fatalf("failed to initialize user store: %v", err)
    }
    defer store.CloseTheUserStore()
    theLogger.Infof("user store (users.db) initialized")

    // 不再需要 isRealUser 命令行参数
    // 前端直接传 user_sn

    chatHandler, err := agent.InitAgent(ctx, cfg, "brain_go_session", defaultLang, theLogger)
    // ...
}
```

### Step 13: 删除旧文件

- [`internal/store/db_opener.go`](internal/store/db_opener.go) — 由 `store/dbcfg/dbcfg.go` 替代
- [`internal/store/db.go`](internal/store/db.go) — 如果之前已创建则删除

---

## 四、影响范围汇总

| 文件 | 操作 |
|------|------|
| `store/dbcfg/dbcfg.go` | **新建** — 包级全局数据库连接管理 |
| `store/the_user_store.go` | **新建** — theUserStore 全局单例 |
| `store/db_opener.go` | **删除** — 由 dbcfg 替代 |
| `store/traits.go` | **重命名** — VectorStore→BrainStore |
| `internal/agent/types.go` | **修改** — SessionManager 大幅精简，switchToUser 简化 |
| `internal/agent/on_chat.go` | **修改** — 使用 dbcfg，ChatAgent 精简 |
| `internal/agent/init.go` | **修改** — 调用 dbcfg.InitDBConfig |
| `internal/agent/on_login.go` | **修改** — 使用 dbcfg.IsRealUser |
| `internal/agent/on_logout.go` | **修改** — switchToUser 简化 |
| `internal/agent/on_session.go` | **修改** — user_no→user_sn |
| `internal/agent/on_msg_new.go` | **修改** — 方法调用名更新 |
| `internal/agent/on_portrait.go` | **修改** — 方法调用名更新 |
| `internal/agent/on_traits.go` | **修改** — 方法名 + BrainStore |
| `internal/agent/on_title.go` | **修改** — 方法调用名更新 |
| `internal/agent/trait_searcher.go` | **修改** — VectorStore→BrainStore |
| `cmd/server/main.go` | **修改** — 命令行参数, dbcfg 初始化, theUserStore |

---

## 五、验证清单

- [ ] `go build ./cmd/server` 编译通过
- [ ] 启动日志 "user store (users.db) initialized"
- [ ] `localdb/users.db` 已创建
- [ ] `--is-real-user=true` 时登录返回 `"test_user_001"`
- [ ] `--is-real-user=false` 时登录返回 `"anonymous"`
- [ ] 登录后聊天正常，`{userSN}.chats.db` / `{userSN}.brain.db` 按需创建
- [ ] 登出后 `user_sn` 为空
- [ ] 所有 `openXxxDB`/`closeXxxDB` 成对出现，无泄漏
- [ ] `db_opener.go` 已删除
- [ ] `AnonymousDBName` 无引用
- [ ] `VectorStore` 全部替换为 `BrainStore`
- [ ] Ctrl+C 停止，无 panic
