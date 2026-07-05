> **（仅为预研，未实际执行）**

# SQLite → PostgreSQL + pgvector 迁移计划

## 附：SN → 自增 ID 的简化改造

用户指出：由于不再需要分库分表（从 per-user .db 变为单一 PG 数据库），原来用于跨文件唯一标识的 **SN（Serial Number）字符串** 也不再需要，所有关联可以改用 `chat_sessions.id`（BIGSERIAL 自增主键，在单库中全局唯一）。

### 当前 SN 使用范围

| 层 | 使用位置 | 用途 |
|----|---------|------|
| **Store** | `chat_messages.chat_sn` | FK → `chat_sessions.sn` |
| **Store** | `web_sources.chat_sn` | FK → `chat_sessions.sn` |
| **Store** | `chat_tags.chat_sn` | FK → `chat_sessions.sn` |
| **Store** | `chat_favorites.chat_sn` | FK → `chat_sessions.sn` |
| **Store** | `traits.chat_sn` | 来源追溯（非 FK，记录来自哪个对话） |
| **Agent** | `session.findChatBySN(sn)` | 在 chats 列表中按 SN 查找 chat |
| **Agent** | `session.switchToChat(sn)` | API 入口：通过 SN 切换到某个历史对话 |
| **Agent** | `persistMessageToDB()` + `db.go` | 消息持久化和 chat 查找 |
| **Agent** | `generateSessionSN()` | 创建新对话时生成全局唯一 SN |
| **Agent** | `on_chat.go`, `on_tag.go`, `on_title.go`, `on_traits.go` | 多个 handler 用 SN 识别目标 chat |
| **Agent** | `isBlankChat()` 判断 `dbChat.SN == ""` | 判断是否为"空白新对话" |
| **Frontend** | `alpine-store.js`, `chat-list.js`, `chat-sse.js`, `chat-sse-responser.js`, `chat.js`, `chat-api.js` | 约 80+ 处使用 `.sn` 标识对话 |
| **Remote** | `on_traits.go` remote-server API | traits 请求中包含 SN |
| **Toolset** | `toolset/string_tl.go: GenerateSN()` | 三因子 SN 生成算法（主机名+时间戳+随机数） |
| **Toolset** | `toolset/string_tl.go: GenerateSNSimple()` | 简易 UUID v4 风格 SN（session ID 用） |

### 替换方案

所有 `chat_sn`/`sn` 替换为 `chat_id`（int64），自增主键 `chat_sessions.id`：

```
变更前：sn = "chat-a1b2c3d4-1a2b3c4d5e6f7g8h-9i0j1k2l3m4n5o6p"
        类型 string，36 字符
        各关联表存 chat_sn TEXT，索引开销大
        前端 API 传 ?sn=xxx 字符串

变更后：id = 42
        类型 int64/BIGINT，8 字节
        各关联表存 chat_id BIGINT，FK + 索引更紧凑
        前端 API 传 ?id=42 整数
```

### 变更细节

#### 数据库 Schema 变更

```diff
- chat_messages.chat_sn TEXT NOT NULL REFERENCES chat_sessions(sn)
+ chat_messages.chat_id BIGINT NOT NULL REFERENCES chat_sessions(id)

- web_sources.chat_sn TEXT NOT NULL REFERENCES chat_sessions(sn)
+ web_sources.chat_id BIGINT NOT NULL REFERENCES chat_sessions(id)

- chat_tags.chat_sn TEXT NOT NULL REFERENCES chat_sessions(sn)
+ chat_tags.chat_id BIGINT NOT NULL REFERENCES chat_sessions(id)

- chat_favorites.chat_sn TEXT NOT NULL REFERENCES chat_sessions(sn)
+ chat_favorites.chat_id BIGINT NOT NULL REFERENCES chat_sessions(id)

- traits.chat_sn TEXT NOT NULL DEFAULT ''
+ traits.chat_id BIGINT NOT NULL DEFAULT 0

- chat_sessions.sn TEXT NOT NULL UNIQUE   ← 移除整列
+ (不需要，id 就是唯一标识)
```

#### Store 层方法签名变更

```go
// 之前：按 SN 操作
func (s *ChatStore) InsertChat(sn string, ...)  → 移除 sn 参数
func (s *ChatStore) FindChatBySN(sn string)     → FindChatByID(id int64)
func (s *ChatStore) LogicDelete(sn string)       → LogicDelete(id int64)
func (s *ChatStore) PhysicalDelete(id int, sn string) → PhysicalDelete(id int)
// 消息也改为 chat_id
func (s *ChatStore) InsertMessage(chatID int64, ...)
func (s *ChatStore) ListMessages(chatID int64)
```

#### Agent 层变更

```go
// session 查找方法
- func (s *session) findChatBySN(sn string) (*store.Chat, *store.ChatStore)
+ func (s *session) findChatByID(id int64) (*store.Chat, *store.ChatStore)

// 空白对话判断
- func (s *session) isBlankChat() bool { return ... dbChat.SN == "" }
+ func (s *session) isBlankChat() bool { return ... dbChat.ID == 0 }

// 消息持久化
- func persistMessageToDB(session *session, msg *Message, chatSN string)
+ func persistMessageToDB(session *session, msg *Message, chatID int64)

// 删除
- func (sm *SessionManager) DeleteMessage(sessionID string, msgID int64) error { ... chatSN := s.currentChat.dbChat.SN ... }
+ func (sm *SessionManager) DeleteMessage(sessionID string, msgID int64) error { ... chatID := s.currentChat.dbChat.ID ... }
```

#### API 接口变更（前后端通信）

所有前端 → 后端的 API 请求中，`sn` 参数改为 `id`：

| API | 当前参数 | 改为 |
|-----|---------|------|
| `GET /api/chat/switch?sn=xxx` | `sn` | `id` |
| `PUT /api/chat/title` body 中的 `sn` | `sn` | `id` |
| `POST /api/chat/traits` body 中的 `sn` | `sn` | `id` |
| `GET /api/chat/title?sn=xxx` | `sn` | `id` |
| SSE `chat_created` 事件中的 `sn` | `sn` | `id` |
| 其他所有 `.sn` 前端引用 | `.sn` | `.id` |

#### 前端数据模型变更

```javascript
// alpine-store.js 中的 ChatItem
{
-  sn: "chat-xxx...",   // 字符串
+  id: 42,              // 整数

-  // 临时 SN 生成逻辑（new_ 前缀）不再需要
-  // promoteBlankItem() 的 sn 更新逻辑改为 id 更新
}
```

#### 可移除的代码

| 文件 | 可移除内容 | 原因 |
|------|-----------|------|
| `toolset/string_tl.go` | `GenerateSN()` 函数 | 不再需要全局唯一 SN |
| `internal/local/agent/db.go` | `generateSessionSN()` 函数 | 不再需要生成 chat SN |
| `internal/local/agent/db.go` | `ensureSessionDBForChat()` 中 `sn := generateSessionSN()` 一行 | 改为使用自增 ID |
| `frontend/static/chat-sse-responser.js` | `onChatCreated()` 中 SN 替换逻辑 | 不再需要 frontSN→realSN 映射 |
| `frontend/static/alpine-store.js` | `promoteBlankItem()` 中临时 SN 生成逻辑 | chat 创建后直接用后端返回的 id |

### 注意事项

1. **前端兼容性**：这是一个破坏性变更，需要同时更新前后端。建议在同一个 PR 中完成，避免 API 不兼容
2. **前端状态管理**：Alpine store 中所有以 `sn` 为 key 的 map/查找（如 `chats.items.find(c => c.sn === sn)`）需要改为 `c.id === id`
3. **ChatStream 管理**：`chat-stream-mgr.js` 中以 `sn` 为 key 管理 stream 对象，需要改为 `id`
4. **Remote-server 接口**：traits 提取请求中不再需要传递 SN，改为传递 `chat_id`
5. **迁移脚本**：数据迁移时，需要将 `chat_messages.chat_sn` → `chat_messages.chat_id` 做外键解析（查找 `chat_sessions.id WHERE sn = X`）
6. **`GenerateSNSimple()` 保留**：session ID 生成仍使用它，不冲突



## 一、项目现状分析

### 当前数据库架构（SQLite + sqlite-vec）

```
localdb/
├── users.db                 # 用户表（单文件，所有用户共享）
│   ├── users                # 用户账户
│   └── roles                # 用户角色（FK -> users.uuid）
│
├── anonymous.chats.db       # 匿名用户聊天数据
│   ├── chat_sessions        # 对话会话
│   ├── chat_messages        # 消息
│   ├── web_sources          # 网络搜索来源
│   ├── chat_tags            # 对话标签
│   └── chat_favorites       # 收藏
│
├── anonymous.brain.db       # 匿名用户特征向量库
│   ├── traits               # 个人特征条目
│   ├── keywords             # 特征关键词
│   └── trait_vectors        # vec0 虚拟表（HNSW向量索引）
│
├── {userNo}.chats.db        # 每个登录用户独立的聊天库（同上结构）
└── {userNo}.brain.db        # 每个登录用户独立的特征向量库（同上结构）
```

### 当前架构的关键特征

1. **每个用户一组独立的 .db 文件**：匿名用户用 `anonymous.*.db`，登录用户用 `{userNo}.{chats|brain}.db`
2. **用户认证集中**：`users.db` 是所有用户共享的唯一数据库
3. **sqlite-vec 向量索引**：通过 `CREATE VIRTUAL TABLE trait_vectors USING vec0(embedding float[2048] distance_metric=cosine)` 实现 HNSW 近似最近邻搜索
4. **会话管理**：`SessionManager` 管理活跃会话，每个 `session` 持有 `chatsStore` 和 `traitsStore` 两个指针
5. **Go 驱动**：`github.com/mattn/go-sqlite3` + `github.com/asg017/sqlite-vec-go-bindings`

### 涉及的源代码文件

| 文件 | 角色 | 变更量 |
|------|------|--------|
| `internal/local/store/scheme.go` | SQLite DDL（chat相关表） | 全量重写 |
| `internal/local/store/chats.go` | ChatStore（Chat CRUD） | 全量重写 |
| `internal/local/store/messages.go` | Message CRUD | 全量重写 |
| `internal/local/store/tags.go` | ChatTags CRUD | 全量重写 |
| `internal/local/store/favorites.go` | Favorites CRUD | 全量重写 |
| `internal/local/store/websource.go` | WebSource CRUD | 全量重写 |
| `internal/local/store/traits.go` | VectorStore + Traits CRUD + 向量搜索 | 全量重写 |
| `internal/local/store/users.go` | UserStore | 全量重写 |
| `internal/local/store/roles.go` | RoleStore | 全量重写 |
| `internal/local/agent/types.go` | session 结构体（持有 per-user stores） | 重大重构 |
| `internal/local/agent/init.go` | InitAgent（初始化匿名 stores） | 重大重构 |
| `internal/local/agent/on_traits.go` | 特征提取 handler（userTraitsDBPath等） | 修改 |
| `internal/local/agent/on_chat.go` | ChatAgent 结构体、NewChatHandler | 轻微修改 |
| `internal/local/config/config.go` | 配置结构体（添加 PG 连接配置） | 新增字段 |
| `cmd/local-server/main.go` | 初始化流程 | 修改 |
| `go.mod` | 依赖管理 | 替换 SQLite 依赖，添加 PG 驱动 |

---

## 二、目标架构（PostgreSQL + pgvector）

### 统一 PostgreSQL 数据库设计

所有用户数据存储在**一个 PostgreSQL 数据库**中，通过 `user_uuid` 列隔离。

```mermaid
erDiagram
    users ||--o{ roles : "uuid"
    users ||--o{ chat_sessions : "uuid"
    users ||--o{ traits : "uuid"
    chat_sessions ||--o{ chat_messages : "sn"
    chat_sessions ||--o{ web_sources : "sn"
    chat_sessions ||--o{ chat_tags : "sn"
    chat_sessions ||--o{ chat_favorites : "sn"
    traits ||--o{ keywords : "id"
    traits ||--o{ trait_vectors : "id"

    users {
        bigserial id PK
        varchar32 uuid UK
        varchar38 nickname
        varchar password
        timestamp create_at
        timestamp update_at
    }
    roles {
        bigserial id PK
        int role_no
        varchar60 role_name
        varchar32 uuid FK
        bool is_public
        bool is_active
        timestamp create_at
        timestamp update_at
    }
    chat_sessions {
        bigserial id PK
        varchar32 uuid FK "新增！用户隔离"
        varchar sn UK
        int role_no
        varchar title
        smallint title_state
        smallint extract_mode
        timestamp extracted_at
        int extracted_count
        bool deleted
        bool pinned
        bool taged
        timestamp create_at
        timestamp update_at
    }
    chat_messages {
        bigserial id PK
        varchar32 uuid FK "新增！用户隔离"
        varchar chat_sn FK
        int group_index
        smallint role
        text reasoning
        text content
        bool extracted
        smallint interrupted
        timestamp create_at
        timestamp update_at
    }
    traits {
        bigserial id PK
        varchar32 uuid FK "新增！用户隔离"
        text trait
        int category
        int confidence
        int half_life
        varchar chat_sn
        timestamp create_at
        timestamp update_at
    }
    trait_vectors {
        bigint trait_id PK FK "对应 traits.id"
        vector2048 embedding "pgvector 向量列"
    }
    keywords {
        bigserial id PK
        varchar word
        int kind
        bigint trait_id FK
        timestamp create_at
    }
    web_sources {
        bigserial id PK
        varchar32 uuid FK "新增！用户隔离"
        varchar chat_sn FK
        bigint msg_id
        text title
        text content
        text url
        text site_name
        text site_icon
        text publish_date
        real score
        timestamp create_at
    }
    chat_tags {
        bigserial id PK
        varchar32 uuid FK "新增！用户隔离"
        varchar chat_sn FK
        varchar tag
        timestamp create_at
    }
    chat_favorites {
        bigserial id PK
        varchar32 uuid FK "新增！用户隔离"
        varchar chat_sn FK
        varchar custom_tag
        timestamp create_at
        timestamp update_at
    }
```

### 关键变化点

1. **所有表新增 `user_uuid` 列**，用来隔离不同用户的数据
2. **统一连接池**，不再有 per-user 文件打开/关闭的开销
3. **pgvector 代替 sqlite-vec**，使用 `vector` 数据类型和索引
4. **PostgreSQL 的 UPSERT / RETURNING** 代替 SQLite 的 `LastInsertId()` 模式
5. **触发器需要换成 PostgreSQL 的 `function + trigger`**

---

## 三、详细实施步骤

### 第 1 步：添加 PostgreSQL + pgvector 依赖

**文件：** [`go.mod`](go.mod)

- 移除：`github.com/mattn/go-sqlite3`, `github.com/asg017/sqlite-vec-go-bindings`
- 添加：`github.com/jackc/pgx/v5`（PostgreSQL 驱动，兼容 sqlx）、`github.com/pgvector/pgvector-go`（pgvector Go 绑定）
- sqlx 对 PostgreSQL 的支持需要通过 `github.com/jmoiron/sqlx` + `github.com/jackc/pgx/v5/stdlib` 桥接

```go
// go.mod 变更
require (
    github.com/jmoiron/sqlx v1.4.0  // 保持，sqlx 同时支持 PG
    github.com/jackc/pgx/v5 v5.7.0
    github.com/pgvector/pgvector-go v0.2.2
)
// 移除: github.com/mattn/go-sqlite3, github.com/asg017/sqlite-vec-go-bindings
```

### 第 2 步：添加数据库配置

**文件：** [`internal/local/config/config.go`](internal/local/config/config.go)

在 `Config` 结构体中新增 `Database` 配置段：

```go
type DatabaseConfig struct {
    DSN string // PostgreSQL 连接字符串，例如 "postgres://user:pass@localhost:5432/brainforever?sslmode=disable"
    
    // 连接池配置
    MaxOpenConns    int // 默认 25
    MaxIdleConns    int // 默认 5
    ConnMaxLifetime int // 分钟，默认 5
}
```

### 第 3 步：重构 Store 层 — 从 per-file 到单连接池

#### 3a. 创建统一的 `DBStore`

**新文件：** [`internal/local/store/db.go`](internal/local/store/db.go)

```go
// DBStore 持有 PostgreSQL 连接池
type DBStore struct {
    db *sqlx.DB
}

// NewDBStore 从 DSN 创建连接池
func NewDBStore(dsn string) (*DBStore, error)

// InitAllSchemas 初始化所有 PostgreSQL 表、索引、pgvector 扩展
func (s *DBStore) InitAllSchemas(dimension int) error

// Close 关闭连接池
func (s *DBStore) Close() error
```

#### 3b. 合并所有 Store 到 `DBStore`

- `ChatStore` → `DBStore` 的方法（添加 `uuid` 参数）
- `UserStore` → `DBStore` 的方法
- `RoleStore` → `DBStore` 的方法
- `VectorStore` → `DBStore` 的方法（移除 per-file 构造）

原本的 `CreateLocalChatScheme(dbFile string)`、`NewUserStore(dbPath string)`、`NewRoleStore(dbPath string)`、`NewVectorStore(dbPath string, ...)` 全部改为 `NewDBStore(dsn string)` 返回的单一实例。

#### 3c. Schema 变更（DDL）

**文件：** [`internal/local/store/scheme.go`](internal/local/store/scheme.go)

将 SQLite DDL 改写为 PostgreSQL DDL：

```sql
-- 启用 pgvector 扩展
CREATE EXTENSION IF NOT EXISTS vector;

-- chat_sessions 表（新增 user_uuid）
CREATE TABLE IF NOT EXISTS chat_sessions (
    id            BIGSERIAL PRIMARY KEY,
    user_uuid     VARCHAR(32) NOT NULL REFERENCES users(uuid),
    sn            VARCHAR(32) NOT NULL UNIQUE,
    role_no       INTEGER NOT NULL,
    title         TEXT NOT NULL DEFAULT '',
    title_state   SMALLINT NOT NULL DEFAULT 0,
    extract_mode  SMALLINT NOT NULL DEFAULT 0,
    extracted_at  TIMESTAMPTZ,
    extracted_count INTEGER NOT NULL DEFAULT 0,
    deleted       BOOLEAN NOT NULL DEFAULT FALSE,
    pinned        BOOLEAN NOT NULL DEFAULT FALSE,
    taged         BOOLEAN NOT NULL DEFAULT FALSE,
    create_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    update_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_sessions_user_uuid ON chat_sessions(user_uuid);

-- traits + trait_vectors（pgvector 表）
CREATE TABLE IF NOT EXISTS traits (
    id          BIGSERIAL PRIMARY KEY,
    user_uuid   VARCHAR(32) NOT NULL REFERENCES users(uuid),
    trait       TEXT NOT NULL,
    category    INTEGER NOT NULL,
    confidence  INTEGER NOT NULL,
    half_life   INTEGER NOT NULL,
    chat_sn     TEXT NOT NULL DEFAULT '',
    create_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    update_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS trait_vectors (
    trait_id  BIGINT PRIMARY KEY REFERENCES traits(id) ON DELETE CASCADE,
    embedding VECTOR(2048)  -- 维度与嵌入模型匹配
);

CREATE INDEX IF NOT EXISTS idx_trait_vectors_hnsw
    ON trait_vectors USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
-- 或使用 HNSW 索引（pgvector >= 0.5.0）：
-- CREATE INDEX IF NOT EXISTS idx_trait_vectors_hnsw
--     ON trait_vectors USING hnsw (embedding vector_cosine_ops);
```

**注意：** 
- SQLite 的 `AUTOINCREMENT` → PostgreSQL 的 `BIGSERIAL`
- SQLite 的 `INTEGER NOT NULL DEFAULT 0` 代表 bool → PostgreSQL 的 `BOOLEAN NOT NULL DEFAULT FALSE`
- SQLite 的 `CURRENT_TIMESTAMP` → PostgreSQL 的 `NOW()`
- SQLite 的 `CREATE TRIGGER ... FOR EACH ROW BEGIN ... END` → PostgreSQL 的 `CREATE OR REPLACE FUNCTION ... CREATE TRIGGER ... EXECUTE FUNCTION ...`
- `vec0` 虚拟表 → pgvector 的真实表和索引

#### 3d. 更新 UserStore — 密码哈希保留

- MD5 哈希逻辑不变
- 查询语法从 `?` 占位符改为 PostgreSQL 的 `$1, $2, ...`
- `LastInsertId()` 改为 `RETURNING id`
- `sql.ErrNoRows` 改为 `pgx.ErrNoRows`（或通过 `sqlx` 的 `ErrNotFound` 判断）

#### 3e. 更新 VectorStore — 向量操作

关键方法变更：

| 当前 (sqlite-vec) | 目标 (pgvector) |
|---|---|
| `sqlite_vec.Auto()` | 不需要，依赖 PG 扩展 |
| `SELECT vec_version()` | `SELECT extversion FROM pg_extension WHERE extname='vector'` |
| `CREATE VIRTUAL TABLE trait_vectors USING vec0(...)` | `CREATE TABLE trait_vectors (trait_id BIGINT PRIMARY KEY, embedding VECTOR(2048))` |
| `INSERT INTO trait_vectors(rowid, embedding) VALUES(?, ?)` 传入 JSON | `INSERT INTO trait_vectors(trait_id, embedding) VALUES($1, $2::vector)` 传入 `[]float32` |
| `SELECT ... FROM trait_vectors v WHERE v.embedding MATCH ? AND k=?` | `SELECT ... FROM trait_vectors v ORDER BY v.embedding <=> $1 LIMIT $2` |
| `v.distance` 返回余弦距离 | `v.embedding <=> $1` 返回余弦距离 |

### 第 4 步：重构 Session 管理

**文件：** [`internal/local/agent/types.go`](internal/local/agent/types.go)

```mermaid
flowchart TD
    A[ChatAgent] --> B[SessionManager]
    B --> C["DBStore (单一 PG 连接池)"]
    C --> D[chat_sessions 表]
    C --> E[chat_messages 表]
    C --> F[traits + trait_vectors 表]
    C --> G[users + roles 表]
    C --> H[其他关联表]
    
    B --> I[session 1: user_uuid=xxx]
    B --> J[session 2: user_uuid=yyy]
    B --> K[session N: user_uuid=zzz]
```

关键变更：

1. **移除 `session.chatsStore` 和 `session.traitsStore` 两个独立指针**
2. **改为 session 结构体中只保留 `user_uuid` 字段**
3. **所有数据库操作通过 `DBStore` 方法并传入 `user_uuid` 参数**

```go
type session struct {
    mu      sync.Mutex
    chatsMu sync.Mutex
    
    id          string      // HTTP cookie session ID
    currentChat *chat
    chats       []store.Chat
    userNo      string      // uuid，用于数据隔离
    // 移除：chatsStore  *store.ChatStore
    // 移除：traitsStore *store.VectorStore
}
```

**数据库操作调用方式变更：**

```go
// 之前（per-user store）
s.chatsStore.ListMessages(chatSN)

// 之后（统一 store + user_uuid 隔离）
store.GlobalDB().ListMessages(ctx, s.userNo, chatSN)
```

### 第 5 步：更新所有 Store 方法的 SQL 语法

将 SQLite 特有的语法迁移到 PostgreSQL：

| SQLite | PostgreSQL |
|--------|-----------|
| `?` 占位符 | `$1, $2, ...` 占位符 |
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL PRIMARY KEY` |
| `DATETIME` | `TIMESTAMPTZ` |
| `CURRENT_TIMESTAMP` | `NOW()` |
| `result.LastInsertId()` | `RETURNING id` + `Scan` |
| `INSERT OR REPLACE` | `INSERT ... ON CONFLICT ... DO UPDATE` |
| `CREATE TRIGGER ... FOR EACH ROW` | `CREATE FUNCTION ... RETURNS TRIGGER ...` + `CREATE TRIGGER ... EXECUTE FUNCTION` |
| 事务 `Begin()` / `Commit()` | 不变（sqlx 兼容） |

### 第 6 步：更新主启动流程

**文件：** [`cmd/local-server/main.go`](cmd/local-server/main.go)

```go
// 之前
userStore, _ := store.NewUserStore("./localdb/users.db")
anonymousStore, _ := store.CreateLocalChatScheme("localdb/anonymous.chats.db")
chatHandler, _ := agent.InitAgent(ctx, cfg, "brain_go_session", defaultLang, theLogger)

// 之后
dbStore, _ := store.NewDBStore(cfg.Database.DSN)
dbStore.InitAllSchemas(cfg.Embedder.Dimension)
chatHandler, _ := agent.InitAgent(ctx, cfg, dbStore, "brain_go_session", defaultLang, theLogger)
```

### 第 7 步：数据迁移脚本

**新文件：** [`cmd/tools/migrate-sqlite-to-pg/main.go`](cmd/tools/migrate-sqlite-to-pg/main.go)

编写独立的迁移工具：

1. 打开所有现有的 `.db` 文件（`users.db`, `anonymous.chats.db`, `anonymous.brain.db`, `{userNo}.chats.db`, `{userNo}.brain.db`）
2. 连接目标 PostgreSQL 数据库
3. 逐表逐行迁移数据
4. 向量数据的特殊处理：从 sqlite-vec 的 `trait_vectors` 表读取 embedding JSON 字段，转为 `[]float32` 写入 pgvector

```mermaid
flowchart LR
    A[users.db] -->|users + roles| P[(PostgreSQL)]
    B[anonymous.chats.db] -->|chat_sessions + chat_messages + etc| P
    C[anonymous.brain.db] -->|traits + keywords + trait_vectors| P
    D["{userNo}.chats.db"] -->|每个用户| P
    E["{userNo}.brain.db"] -->|每个用户| P
    
    P --> F[统一的 brainforever 数据库]
```

### 第 8 步：代码清理与测试

1. 移除 sqlite3 相关代码（import 和初始化）
2. 移除 `localdb/` 目录创建逻辑
3. 更新 `go.mod` 依赖
4. 端到端测试所有 API 端点
5. 性能回归测试（特别是向量搜索）

---

## 四、实施顺序与工作分解

### 总 TODO 清单

```
[ ] 第 1 步：更新 go.mod —— 添加 pgx + pgvector，移除 sqlite3 + sqlite-vec
[ ] 第 2 步：添加 DatabaseConfig 到 config.go
[ ] 第 3 步：创建 internal/local/store/db.go —— 统一 DBStore + 连接池
[ ] 第 4 步：重写 scheme.go —— 所有表 DDL 改为 PostgreSQL DDL
[ ] 第 5 步：重写 users.go + roles.go —— 改用 PG 语法 + 统一 DBStore
[ ] 第 6 步：重写 chats.go —— 添加 user_uuid 隔离 + PG 语法
[ ] 第 7 步：重写 messages.go —— PG 语法
[ ] 第 8 步：重写 tags.go + favorites.go + websource.go —— PG 语法
[ ] 第 9 步：重写 traits.go —— pgvector 替代 sqlite-vec
[ ] 第 10 步：重构 agent/types.go —— 移除 per-user stores，改为 user_uuid 隔离
[ ] 第 11 步：重构 agent/init.go —— 传递 DBStore 而非 anonymousStore
[ ] 第 12 步：修改 agent/on_traits.go —— 移除 userTraitsDBPath，改用 DBStore
[ ] 第 13 步：修改 cmd/local-server/main.go —— 初始化 DBStore 替代多文件
[ ] 第 14 步：创建 cmd/tools/migrate-sqlite-to-pg —— 数据迁移脚本
[ ] 第 15 步：更新 deploy/local-server.toml.example —— 添加 PG DSN 配置说明
[ ] 第 16 步：运行数据迁移，端到端测试
```

### 并行任务建议

- **第 1-2 步**可最先并行完成（依赖注入）
- **第 4-9 步**是核心 SQL 改造，可以按独立文件并行，但建议按顺序逐个完成以避免遗漏
- **第 10-11 步**是架构结构调整，需要第 3-9 步完成后才能开始
- **第 14 步**迁移脚本可以和第 4-9 步并行开发，因为迁移逻辑独立

### 实施注意事项

1. **事务处理的差异**：SQLite 的 `defer tx.Rollback()` 模式在 PG 中同样适用，但要注意 PG 的 `Repeatable Read` 隔离级别与 SQLite 默认的 `Serializable` 的区别
2. **时间处理**：SQLite 的 `DATETIME` 不带时区，PG 的 `TIMESTAMPTZ` 带时区。需要确保所有时间存储为 UTC
3. **全文搜索**：目前项目未使用 SQLite FTS，所以不需要迁移到 PG 的全文搜索
4. **自增 ID**：PG 的 `BIGSERIAL` 在分布式场景可能出现空洞，但当前项目是单实例，不影响
5. **匿名用户处理**：匿名用户需要分配一个固定的 `user_uuid`（如 `__anonymous__` 或随机 UUID），在 `users` 表中不需要有记录，但数据隔离需要这个值

### 是否需要保留 SQLite 兼容？

不建议同时支持两种数据库（双驱动模式），这会使代码复杂度和维护成本翻倍。建议：
- **一次性切换**：所有环境统一迁移到 PostgreSQL
- **迁移脚本**：提供从当前 SQLite 数据导出到 PostgreSQL 的独立工具
- **回滚方案**：保留当前的 SQLite 代码分支作为 tag（如 `v1-sqlite`），如果 PG 出现严重问题可以回退
