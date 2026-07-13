# SQLite + MySQL → PostgreSQL + pgvector 统一迁移方案

> **最后更新**: 2026-07-13
> **目标**: 将当前 SQLite（per-user chats.db/brain.db）+ MySQL（users 表）统一迁移到 **单实例 PostgreSQL + pgvector**
> **前提**: 不需要迁移历史数据，新部署直接使用新数据库

---

## 一、现状分析

### 1.1 当前双数据库架构

```
┌─────────────────────────────────────────────────────┐
│                  当前架构概览                          │
├─────────────────────────────────────────────────────┤
│                                                       │
│  MySQL (全局)           SQLite (per-user)              │
│  ┌──────────────┐     ┌──────────────────────┐        │
│  │  users 表     │     │ {userSN}.chats.db     │        │
│  │  roles 表     │     │ ├─ chat_sessions      │        │
│  │  (用户+角色)   │     │ ├─ chat_messages       │        │
│  └──────────────┘     │ ├─ web_sources         │        │
│                        │ ├─ chat_tags           │        │
│  ✅ 角色表已迁到 MySQL  │ └─ chat_favorites      │        │
│  ❌ `store/roles.go`   │                        │        │
│     是死代码(已废弃)     │ {userSN}.brain.db      │        │
│                        │ ├─ traits              │        │
│                        │ ├─ keywords            │        │
│                        │ └─ trait_vectors       │        │
│                        │    (sqlite-vec vec0)   │        │
│                        └──────────────────────┘        │
└─────────────────────────────────────────────────────┘
```

### 1.2 涉及的源代码文件清单

| 层 | 文件 | 角色 | 变更类型 |
|----|------|------|---------|
| **Store** | [`internal/store/mysqldb.go`](internal/store/mysqldb.go) | MySQL 全局连接单例 | 替换为 PostgreSQL |
| **Store** | [`internal/store/users.go`](internal/store/users.go) | UserStore（当前用 MySQL） | 重写 SQL 语法 |
| **Store** | [`internal/store/roles.go`](internal/store/roles.go) | RoleStore（死代码，无人引用） | **删除** |
| **Store** | [`internal/store/chats.go`](internal/store/chats.go) | ChatStore（per-user SQLite） | 全量重写 |
| **Store** | [`internal/store/messages.go`](internal/store/messages.go) | Message CRUD | 全量重写 |
| **Store** | [`internal/store/tags.go`](internal/store/tags.go) | ChatTags CRUD | 全量重写 |
| **Store** | [`internal/store/favorites.go`](internal/store/favorites.go) | Favorites CRUD | 全量重写 |
| **Store** | [`internal/store/websource.go`](internal/store/websource.go) | WebSource CRUD | 全量重写 |
| **Store** | [`internal/store/traits.go`](internal/store/traits.go) | BrainStore（sqlite-vec 向量搜索） | 全量重写 → pgvector |
| **Store** | [`internal/store/brain_scheme.go`](internal/store/brain_scheme.go) | BrainStore DDL（vec0 虚拟表） | 全量重写 |
| **Store** | [`internal/store/chats_scheme.go`](internal/store/chats_scheme.go) | ChatStore DDL（SQLite 语法） | 全量重写 |
| **Store** | [`internal/store/dbc/dbc.go`](internal/store/dbc/dbc.go) | 包级全局 DB 管理器 | **移除**（不再需要 per-user 模式） |
| **Store** | [`internal/store/dbpath/dbpath.go`](internal/store/dbpath/dbpath.go) | 路径构造工具 | **移除**（不再需要 per-user 文件路径） |
| **Agent** | [`internal/agent/db.go`](internal/agent/db.go) | session SN 生成 + 持久化辅助 | 重大重构 |
| **Agent** | [`internal/agent/init.go`](internal/agent/init.go) | 初始化流程 | 修改 |
| **Agent** | [`internal/agent/on_chat.go`](internal/agent/on_chat.go) | openChatDB/openBrainDB 辅助方法 | 移除 per-user 打开模式 |
| **Agent** | [`internal/agent/on_msg_new.go`](internal/agent/on_msg_new.go) | 消息处理 | 修改 |
| **Agent** | [`internal/agent/on_traits.go`](internal/agent/on_traits.go) | 特征提取 | 修改 |
| **Agent** | [`internal/agent/on_title.go`](internal/agent/on_title.go) | 标题处理 | 修改 |
| **Agent** | [`internal/agent/on_tag.go`](internal/agent/on_tag.go) | 标签处理 | 修改 |
| **Agent** | [`internal/agent/on_favorites.go`](internal/agent/on_favorites.go) | 收藏处理 | 修改 |
| **Agent** | [`internal/agent/trait_searcher.go`](internal/agent/trait_searcher.go) | 特征搜索适配器 | 修改 |
| **Session** | [`internal/session/session.go`](internal/session/session.go) | Session 状态管理 | 修改 |
| **User** | [`internal/user/login.go`](internal/user/login.go) | 登录流程 | 修改（移除 `dbc.InitUserDB`） |
| **User** | [`internal/user/logout.go`](internal/user/logout.go) | 登出流程 | 轻微修改 |
| **Main** | [`cmd/server/main.go`](cmd/server/main.go) | 启动入口 | 修改初始化流程 |
| **Config** | [`internal/config/config.go`](internal/config/config.go) | 配置结构体 | 修改 |
| **部署** | [`deploy/settings_template/server.template.toml`](deploy/settings_template/server.template.toml) | 配置模板 | 更新 |

---

## 二、目标架构

### 2.1 单一 PostgreSQL 数据库

```mermaid
erDiagram
    users ||--o{ chat_sessions : "user_id"
    users ||--o{ traits : "user_id"
    chat_sessions ||--o{ chat_messages : "id"
    chat_sessions ||--o{ web_sources : "id"
    chat_sessions ||--o{ chat_tags : "id"
    chat_sessions ||--o{ chat_favorites : "id"
    traits ||--o{ trait_vectors : "id"
    traits ||--o{ keywords : "id"

    users {
        bigserial id PK
        varchar32 sn UK "用户序列号"
        varchar6 no UK "6位用户编号"
        varchar18 tel
        varchar38 nickname
        varchar password "MD5+salt"
        varchar32 salt
        boolean deleted
        int settings_ver
        jsonb settings
        timestamptz create_at
        timestamptz update_at
    }
    chat_sessions {
        bigserial id PK
        varchar sn UK "对外标识符【保留，防止泄露 id 规模】"
        bigint user_id FK "-> users.id  【代替 per-user 文件】"
        bigint role_no
        varchar title
        smallint title_state
        smallint extract_mode
        timestamptz extracted_at
        int extracted_count
        boolean deleted
        boolean pinned
        boolean taged
        timestamptz create_at
        timestamptz update_at
    }
    chat_messages {
        bigserial id PK
        bigint chat_id FK "-> chat_sessions.id"
        int group_index
        smallint role
        text reasoning
        text content
        boolean extracted
        smallint interrupted
        timestamptz create_at
        timestamptz update_at
    }
    web_sources {
        bigserial id PK
        bigint chat_id FK "-> chat_sessions.id"
        bigint msg_id
        text title
        text content
        text url
        text site_name
        text site_icon
        text publish_date
        real score
        timestamptz create_at
    }
    chat_tags {
        bigserial id PK
        bigint chat_id FK "-> chat_sessions.id"
        varchar tag
        timestamptz create_at
    }
    chat_favorites {
        bigserial id PK
        bigint chat_id FK "-> chat_sessions.id"
        varchar custom_tag
        timestamptz create_at
        timestamptz update_at
    }
    traits {
        bigserial id PK
        bigint user_id FK "-> users.id  【代替 per-user 文件】"
        text trait
        int category
        int confidence
        int half_life
        int privacy_level
        bigint chat_id "-> chat_sessions.id 【代替 chat_sn 字符串】"
        timestamptz create_at
        timestamptz update_at
    }
    trait_vectors {
        bigint trait_id PK FK "-> traits.id"
        vector embedding "pgvector 向量列"
    }
    keywords {
        bigserial id PK
        varchar word
        int kind
        bigint trait_id FK "-> traits.id"
        timestamptz create_at
    }
```

### 2.2 架构变化要点

| 维度 | 当前 | 目标 |
|------|------|------|
| 数据库种类 | 2种（SQLite + MySQL） | 1种（PostgreSQL） |
| 数据库实例 | 每个用户 2 个 SQLite 文件 + 1 个 MySQL 连接 | 1 个 PostgreSQL 连接池 |
| 数据隔离 | 文件隔离（每个用户独立 .db） | 行级隔离（`user_id` 列） |
| 向量搜索 | sqlite-vec（vec0 虚拟表） | pgvector（VECTOR 数据类型 + HNSW 索引） |
| 连接管理 | 按需打开/关闭 per-user 文件 | 单一连接池，常驻打开 |
| chat 对外标识 | SN 字符串（chat-xxx 格式） | **保留 SN**（防止泄露业务规模） |
| chat 内部 FK | `chat_messages.chat_id` 用 `id` | 不变 |
| 驱动 | `mattn/go-sqlite3` + `go-sql-driver/mysql` | `pgx/v5` + `pgvector-go` |

---

## 三、关键变更详解

### 3.1 依赖变更

**`go.mod`**:
- **移除**: `github.com/mattn/go-sqlite3`, `github.com/go-sql-driver/mysql`, `github.com/asg017/sqlite-vec-go-bindings`
- **添加**: `github.com/jackc/pgx/v5`, `github.com/pgvector/pgvector-go`
- 说明：`sqlx` 对 pgx 的支持通过 `pgx/stdlib` 桥接，sqlx 本身的 import 路径不变

### 3.2 SQL 语法对照

| SQLite | PostgreSQL |
|--------|-----------|
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL PRIMARY KEY` |
| `DATETIME` | `TIMESTAMPTZ` |
| `CURRENT_TIMESTAMP` | `NOW()` |
| `?` 占位符 | `$1, $2, ...` 占位符 |
| `result.LastInsertId()` | `INSERT ... RETURNING id` + `.Scan(&id)` |
| `INTEGER NOT NULL DEFAULT 0` (bool) | `BOOLEAN NOT NULL DEFAULT FALSE` |
| `CREATE TRIGGER ... FOR EACH ROW BEGIN ... END` | `CREATE FUNCTION ... RETURNS TRIGGER ...` + `CREATE TRIGGER ... EXECUTE FUNCTION ...` |
| `ON DELETE CASCADE` | `ON DELETE CASCADE`（功能兼容） |
| `VACUUM` | 不需要（PG 自动 MVCC 清理） |
| `sqlite-vec: vec0 VIRTUAL TABLE` | `pgvector: VECTOR(n) 列 + HNSW 索引` |
| `v.embedding MATCH ? AND k=?` | `ORDER BY v.embedding <=> $1 LIMIT $2` |

### 3.3 向量搜索：sqlite-vec → pgvector

```go
// 当前 (sqlite-vec)
sqlite_vec.Auto()
db, _ := sqlx.Open("sqlite3", dbPath+"?...")
// 建表
CREATE VIRTUAL TABLE trait_vectors USING vec0(
    embedding float[2048] distance_metric=cosine
);
// 插入
INSERT INTO trait_vectors(rowid, embedding) VALUES(?, ?)  // embedding 是 JSON 字符串
// 搜索
SELECT v.rowid, v.distance FROM trait_vectors v 
WHERE v.embedding MATCH ? AND k=? 
ORDER BY v.distance

// 目标 (pgvector)
db, _ := sqlx.Open("pgx", dsn)
// 建表
CREATE TABLE trait_vectors (
    trait_id  BIGINT PRIMARY KEY REFERENCES traits(id) ON DELETE CASCADE,
    embedding VECTOR(2048)
);
CREATE INDEX ON trait_vectors USING hnsw (embedding vector_cosine_ops);
// 插入
INSERT INTO trait_vectors(trait_id, embedding) VALUES($1, $2::vector)  // embedding 是 []float32
// 搜索
SELECT v.trait_id, v.embedding <=> $1 AS distance
FROM trait_vectors v
ORDER BY v.embedding <=> $1
LIMIT $2
```

### 3.4 架构模式变更：per-user 文件 → 统一连接池

```mermaid
flowchart TD
    subgraph 当前 [当前: per-user SQLite 文件]
        A[请求到达] --> B[openChatDB userSN]
        B --> C[打开 {userSN}.chats.db]
        C --> D[执行查询]
        D --> E[closeChatDB]
        E --> F[请求响应]
        
        A --> G[openBrainDB userSN]
        G --> H[打开 {userSN}.brain.db]
        H --> I[执行查询]
        I --> J[closeBrainDB]
    end

    subgraph 目标 [目标: 统一 PostgreSQL 连接池]
        K[请求到达] --> L[从连接池获取连接]
        L --> M["WHERE user_id = $1 行级隔离"]
        M --> N[执行查询]
        N --> O[归还连接到池]
        O --> P[请求响应]
    end
```

**核心变化点**:
1. 移除 [`store/dbc/dbc.go`](internal/store/dbc/dbc.go) 中的 `OpenLocalChatDB` / `OpenLocalBrainDB` / `InitUserDB` 等 per-user 函数
2. 创建统一的全局 `*sqlx.DB` 连接池单例（类似当前 `theMySQLDBC` 的模式）
3. 所有 Store 方法不再需要 `userID` + `userSN` 参数来定位文件，改为统一的 `userID` 参数用于行级过滤
4. 消除 agent 层 `openChatDB(sess)` / `openBrainDB(sess)` 的临时打开/关闭模式

### 3.5 SN 字段保留 + 前端临时 SN 模式分析

#### 设计决策

**保留 `chat_sessions.sn` 作为对外标识符**，理由：
1. **防止泄露业务规模**：自增 `id` 让用户能通过 ID 大小猜测系统有多少对话，`sn` 作为不透明字符串可以避免此问题
2. **保持前端 API 不变**：所有 `?sn=xxx` 参数继续使用，前后端适配工作量大幅降低
3. **内部仍用 `id` 做 FK**：`chat_messages.chat_id` 引用 `chat_sessions.id`，性能更好

#### 前后端数据通道

```
chat_sessions 表:
  id       BIGSERIAL PK     ← 内部自增主键（不对外暴露）
  sn       VARCHAR(32) UK   ← 对外唯一标识（前端 API、SSE 事件使用）
  user_id  BIGINT FK        ← 用户隔离
  ...

chat_messages 表:
  id       BIGSERIAL PK
  chat_id  BIGINT FK → chat_sessions.id    ← 用 id 做 FK（性能好）
  ...

traits 表:
  id       BIGSERIAL PK
  chat_sn  VARCHAR(32) → chat_sessions.sn  ← 用 sn 做引用（可读性好）
  ...
```

#### SN 生成策略简化

当前 `toolset.GenerateSN("chat-")` 使用三因子算法（主机名+时间戳+随机数），这是为了在分布式多实例间保证唯一。

迁移到单 PG 后，SN 的唯一性可由数据库 `UNIQUE` 约束保证，生成逻辑可以简化：
- 使用 `nanoid` 风格或 `UUID v4` 短格式（如 `ch_` + 16 位随机字符串）
- 长度从 36 字符缩短到 20 字符左右
- 如果发生碰撞（概率极低），PG 的 `UNIQUE` 约束会报错，重试即可

#### "前端临时 SN"模式保持不变

```mermaid
sequenceDiagram
    participant 前端
    participant 后端
    participant PG

    前端->>前端: 点击"新对话"\n blankItem.sn = ''
    前端->>前端: 发第一条消息\n promoteBlankItem()\n 生成临时 front_sn='new_...'
    前端->>后端: POST /api/chat { front_sn, message }
    后端->>后端: ensureSessionDBForChat()\n 生成 sn + INSERT
    后端->>PG: INSERT INTO chat_sessions(sn, ...)
    PG-->>后端: id=42 (内部 PK)
    后端-->>前端: SSE chat_created { sn: "ch_xxx", front_sn: "new_..." }
    前端->>前端: onChatCreated()\n 替换 front_sn → 真实 sn
```

唯一变化：后端 SN 生成从 `toolset.GenerateSN("chat-")` 变为更轻量的实现。

### 3.6 前端 API 变更

所有涉及 `sn` 的 API 需要改为 `id`：

| API | 当前参数 | 改为 |
|-----|---------|------|
| `GET /api/chat/switch` | `?sn=xxx` | `?id=123` |
| `PUT /api/chat/title` | `?sn=xxx` | `?id=123` |
| `GET /api/session/title` | `?sn=xxx` | `?id=123` |
| `PUT /api/chat/pin` | `?sn=xxx` | `?id=123` |
| `DELETE /api/chat` | `?sn=xxx` | `?id=123` |
| `PUT /api/chat/restore` | `?sn=xxx` | `?id=123` |
| `DELETE /api/chat/permanent` | `?sn=xxx` | `?id=123` |
| `POST /api/chat/traits` | `{"sn":"xxx"}` | `{"id":123}` |
| SSE `chat_created` 事件 | `sn` + `front_sn` | 不变 |

**前端变更大幅简化**：API 参数保持 `sn`，数据模型保持 `sn`，仅需关注：
- `traits` 相关 API 的 `sn` 参数确认不变（当前 `POST /api/chat/traits` 使用 `{"sn":"xxx"}`）
- 无其他前端数据结构变更

---

## 四、pgvector 向量搜索方案

### 4.1 为什么选择 pgvector

| 特性 | pgvector | MySQL VECTOR | 独立向量库 |
|------|----------|-------------|-----------|
| 成熟度 | ✅ 生产级（2019 年至今） | ⚠️ 实验性（MySQL 9.0+） | ✅ |
| 距离函数 | L2, cosine, inner product | cosine, L2 | 多种 |
| 索引类型 | HNSW, IVFFlat | HNSW | 多种 |
| 集成复杂度 | 低（同一数据库） | 低 | 高（独立运维） |
| 是否需要新驱动 | 是（pgvector-go） | 否（MySQL 驱动已存在） | 是 |
| Go 生态 | ✅ pgvector-go 成熟 | ❌ 无官方 Go 绑定 | 视具体产品 |

**结论**: pgvector 是最佳选择，与 PostgreSQL 深度集成，运维简单，社区活跃。

### 4.2 pgvector 使用示例

```go
import (
    "github.com/jmoiron/sqlx"
    "github.com/pgvector/pgvector-go"
    _ "github.com/jackc/pgx/v5/stdlib" // 通过 pgx 驱动桥接 sqlx
)

// 插入向量
embedding := pgvector.NewVector(float32s)  // []float32
_, err := db.Exec(
    "INSERT INTO trait_vectors(trait_id, embedding) VALUES($1, $2)",
    traitID, embedding,
)

// 向量搜索（余弦距离）
rows, err := db.Query(
    `SELECT t.id, t.trait, t.category, 
            v.embedding <=> $1 AS distance
     FROM trait_vectors v
     JOIN traits t ON t.id = v.trait_id
     WHERE t.user_id = $2
     ORDER BY distance
     LIMIT $3`,
    queryVector, userID, topK,
)

// 创建 HNSW 索引（pgvector ≥ 0.5.0）
CREATE INDEX ON trait_vectors USING hnsw (embedding vector_cosine_ops);
```

### 4.3 关于 `ON UPDATE CURRENT_TIMESTAMP` 触发器的替代

SQLite 的 `CREATE TRIGGER ... FOR EACH ROW BEGIN ... END` 在 PostgreSQL 中需要改为函数+触发器：

```sql
-- PostgreSQL 等效
CREATE OR REPLACE FUNCTION update_update_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.update_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_chat_sessions_update_at
    BEFORE UPDATE ON chat_sessions
    FOR EACH ROW
    EXECUTE FUNCTION update_update_at_column();
```

---

## 五、实施步骤

### 第 1 步：依赖与环境准备

- 更新 [`go.mod`](go.mod)：移除 sqlite3/mysql 驱动，添加 pgx + pgvector-go
- 更新 [`internal/config/config.go`](internal/config/config.go)：
  - 修改 `DatabaseConfig` 结构体，添加 PG 连接池参数
  - 可移除 `DataConfig.Dir`（不再需要 localdb 目录）
  - 添加 `InitPostgresDB(dsn)` / `ThePGDB()` / `ClosePGDB()` 替代 `mysqldb.go`
- 更新 [`deploy/settings_template/server.template.toml`](deploy/settings_template/server.template.toml)：更新数据库配置段

### 第 2 步：创建 PostgreSQL Schema

- 编写新文件（或合并到现有文件）统一 DDL
- 所有表添加 `user_id` 列（BIGINT NOT NULL REFERENCES users(id)）
- 创建 pgvector 扩展 + HNSW 索引
- 创建 PostgreSQL 触发器函数

### 第 3 步：重构 Store 层

- 创建统一 `DBStore` 或直接用包级全局 `*sqlx.DB`
- 按文件逐个重写：
  - [`internal/store/users.go`](internal/store/users.go) — 修改 SQL 语法
  - [`internal/store/roles.go`](internal/store/roles.go) — **删除**（死代码，无引用）
  - [`internal/store/chats.go`](internal/store/chats.go) — ChatStore → 统一 PG 连接 + `user_id` 过滤
  - [`internal/store/messages.go`](internal/store/messages.go) — SQL 语法迁移
  - [`internal/store/tags.go`](internal/store/tags.go) — SQL 语法迁移
  - [`internal/store/favorites.go`](internal/store/favorites.go) — SQL 语法迁移
  - [`internal/store/websource.go`](internal/store/websource.go) — SQL 语法迁移
  - [`internal/store/traits.go`](internal/store/traits.go) — BrainStore → pgvector 改造

### 第 4 步：移除 per-user 文件管理

- **移除** `internal/store/dbc/dbc.go` 整个包（`InitUserDB`, `OpenLocalChatDB`, `OpenLocalBrainDB` 等）
- **移除** `internal/store/dbpath/dbpath.go` 整个包（`ForUser`, `DirID` 等）
- 所有调用方改为直接使用全局 PG 连接

### 第 5 步：重构 Agent/Session 层

- 移除 `openChatDB(sess)` / `openBrainDB(sess)` 辅助方法
- 改为直接使用全局 PG 连接（通过包级函数获取）
- 移除 `generateSessionSN()`（chat SN 生成）
- 修改 `ensureSessionDBForChat()` — 使用 `INSERT ... RETURNING id`
- 修改 `persistMessageToDB()` — 使用 `chat_id` 而非 `chatSN`
- 修改 `session.FindChatBySN()` → `FindChatByID()`
- 修改 `session.SwitchToChat(sn)` → `SwitchToChat(id)`
- 修改 `session.IsBlankChat()` — 用 `DBCHat.ID == 0` 替代 `SN == ""`

### 第 6 步：重构 Login/Logout

- 修改 [`internal/user/login.go`](internal/user/login.go) 中 `afterLogin()`：
  - 移除 `dbc.InitUserDB()` 调用（不再需要创建 per-user 文件）
  - 改为直接从 PG 加载聊天列表（`WHERE user_id = $1 ORDER BY ...`）

### 第 7 步：重构 Main 入口

- 修改 [`cmd/server/main.go`](cmd/server/main.go)：
  - 移除 `os.MkdirAll(cfg.Data.Dir, ...)`（不需要 localdb 目录）
  - `store.InitMySQLDB()` → `store.InitPostgresDB()`
  - `store.CloseMySQLDB()` → `store.ClosePGDB()`

### 第 8 步：前端 API 适配

- 所有 `?sn=xxx` 参数改为 `?id=123`
- 所有 `.sn` 数据模型字段改为 `.id`
- 移除前端临时 SN 生成逻辑

### 第 9 步：代码清理

- 清理所有不再需要的 import（sqlite3, mysql 驱动）
- 移除 `toolset.GenerateSN()` 如果不再需要（确认 session ID 是否改用其他方式）
- 移除 `localdb/` 目录创建和引用

### 第 10 步：端到端验证

- 启动空 PG 数据库，验证所有 API 端点正常工作
- 验证向量搜索（特征提取 + 特征搜索）功能正常
- 验证用户登录/登出/多用户数据隔离正确

---

## 六、风险与注意事项

### 6.1 已知风险

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| `?` 占位符 → `$N` | 所有 SQL 查询需要逐个修改 | 全局搜索 `?` 替换为 `$N`，注意不要遗漏嵌套查询 |
| `LastInsertId()` 不可用 | 需改为 `RETURNING id` + `Scan` | 逐个方法检查，使用 `sqlx` 的 `QueryRow` 或 `Get` |
| SQLite bool (0/1) → PG bool | 需要检查所有 bool 列的读写 | PG 的 `BOOLEAN` 接受 `true/false` 也接受 `0/1`，但建议显式使用布尔值 |
| sqlite-vec 的 `vec_version()` | 需要替换为 pgvector 版本检测 | `SELECT extversion FROM pg_extension WHERE extname='vector'` |
| SQLite 触发器语法 | PostgreSQL 需要函数包装 | 为每个 `update_at` 触发器创建通用函数 |
| `defer tx.Rollback()` 模式 | PostgreSQL 事务语义有差异 | 保持 `Rollback()` 模式，PG 也会在 `Commit` 或 `Rollback` 后正确释放 |

### 6.2 注意事项

1. **pgvector 版本**: 需要 pgvector ≥ 0.5.0 以获得 HNSW 索引支持（比 IVFFlat 性能更好）
2. **PostgreSQL 版本**: 建议 PostgreSQL ≥ 15（pgvector 0.5+ 支持良好）
3. **匿名用户无需处理**: 当前系统匿名用户无法进行任何数据操作（聊天/特征提取等均需登录），因此没有匿名数据遗留问题。迁移后登录用户直接写入 PG 即可
4. **session ID 生成**: `toolset.GenerateSNSimple()`（用于 session ID）应该保留，它不影响数据库架构
5. **单库性能**: 当用户量很大时，单表 `chat_messages` 可能非常庞大。但考虑到本项目定位（个人笔记/轻量级AI助手），单库足够。未来如需扩展，可以通过用户ID分表或Citus分片
6. **连接池参数**: 使用 `pgx` 连接池的默认配置（`MaxConns=5`），可根据负载调整

---

## 七、实施顺序建议

### 推荐执行顺序（按依赖关系）

```
Step 1: 依赖 & 配置
  ├── go.mod 更新（pgx + pgvector-go）
  └── config.go 更新（PG DSN 配置）

Step 2: Store 层重构（可并行）
  ├── PG 连接单例（替代 mysqldb.go）
  ├── 统一 Schema DDL
  ├── users.go + roles.go（SQL 语法 + 合并）
  ├── chats.go + chats_scheme.go（PG 语法 + user_id 隔离）
  ├── messages.go（PG 语法）
  ├── tags.go（PG 语法）
  ├── favorites.go（PG 语法）
  ├── websource.go（PG 语法）
  ├── traits.go + brain_scheme.go（pgvector 改造）
  └── 移除 dbc/dbpath 包

Step 3: Agent/Session 层重构
  ├── session.go（SN → ID）
  ├── db.go（移除 SN 生成，简化持久化）
  ├── on_chat.go（移除 openChatDB/openBrainDB）
  ├── on_msg_new.go
  ├── on_traits.go
  ├── on_title.go / on_tag.go / on_favorites.go
  └── trait_searcher.go

Step 4: 用户层重构
  ├── login.go（移除 dbc.InitUserDB）
  └── logout.go

Step 5: Main 入口 & 部署配置
  ├── main.go（初始化 PG）
  └── server.template.toml

Step 6: 前端适配
  ├── alpine-store.js（sn → id）
  ├── chat-api.js（API 参数 sn → id）
  ├── chat-sse-responser.js
  ├── chat-stream-mgr.js
  └── 其余前端文件

Step 7: 清理 & 验证
  ├── 移除未使用的 import / 函数
  ├── 移除 localdb 目录引用
  └── 端到端测试
```

---

## 八、配置变更示例

### `server.template.toml` 更新

```toml
# ============================================================
# 数据库配置变更
# ============================================================
[database]
# 之前: MySQL
# dsn = "user:password@tcp(127.0.0.1:3306)/brain_forever"

# 之后: PostgreSQL
# 通过环境变量 PG_DSN 设置，格式:
#   postgres://user:password@127.0.0.1:5432/brain_forever?sslmode=disable
dsn = ""

# 连接池配置
max_open_conns = 25
max_idle_conns = 5

# 注意: [data] 段可以移除（不再需要 localdb 目录）
```

### `config.go` 更新

```go
type DatabaseConfig struct {
    DSN          string // "postgres://user:pass@localhost:5432/brain_forever"
    MaxOpenConns int    // 默认 25
    MaxIdleConns int    // 默认 5
}
```

---

## 九、PostgreSQL 完整 DDL 初始化脚本

以下为完整的数据库建表 SQL，**无需迁移历史数据**时直接在新 PG 实例上执行即可。

```sql
-- ============================================================
-- 0. 扩展
-- ============================================================
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS vector;

-- 验证 pgvector 安装
SELECT extversion FROM pg_extension WHERE extname = 'vector';

-- ============================================================
-- 1. 通用函数：自动更新 update_at
-- ============================================================

CREATE OR REPLACE FUNCTION update_update_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.update_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ============================================================
-- 2. 用户表（从 MySQL 迁移）
-- ============================================================

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    sn            VARCHAR(32)  NOT NULL UNIQUE,       -- 用户序列号
    no            VARCHAR(6)   NOT NULL UNIQUE,        -- 6位用户编号
    tel           VARCHAR(18)  NOT NULL DEFAULT '',
    nickname      VARCHAR(38)  NOT NULL,
    password      VARCHAR(32)  NOT NULL,               -- MD5 + salt
    salt          VARCHAR(32)  NOT NULL,
    deleted       BOOLEAN      NOT NULL DEFAULT FALSE,
    settings_ver  INTEGER      NOT NULL DEFAULT 0,
    settings      JSONB        NOT NULL DEFAULT '{}',
    create_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    update_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_sn   ON users(sn);
CREATE INDEX IF NOT EXISTS idx_users_no   ON users(no);
CREATE INDEX IF NOT EXISTS idx_users_tel  ON users(tel);

CREATE TRIGGER trg_users_update_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_update_at_column();

-- ============================================================
-- 3. 角色表（从 MySQL 迁移，roles.go 死代码删除后仅保留此表）
-- ============================================================

CREATE TABLE IF NOT EXISTS roles (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_no    INTEGER      NOT NULL,
    role_name  VARCHAR(60)  NOT NULL,
    uuid       VARCHAR(32)  NOT NULL,
    is_public  BOOLEAN      NOT NULL DEFAULT FALSE,
    is_active  BOOLEAN      NOT NULL DEFAULT TRUE,
    create_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    update_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_roles_user_id ON roles(user_id);

CREATE TRIGGER trg_roles_update_at
    BEFORE UPDATE ON roles
    FOR EACH ROW
    EXECUTE FUNCTION update_update_at_column();

-- ============================================================
-- 4. 对话会话表（从 per-user chats.db 合并）
--    sn 保留作为对外标识符，防止泄露 id 业务规模
-- ============================================================

CREATE TABLE IF NOT EXISTS chat_sessions (
    id              BIGSERIAL PRIMARY KEY,
    sn              VARCHAR(32)  NOT NULL UNIQUE,       -- 对外标识符（不透明字符串）
    user_id         BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_no         BIGINT       NOT NULL DEFAULT 0,
    title           TEXT         NOT NULL DEFAULT '',
    title_state     SMALLINT     NOT NULL DEFAULT 0,
    extract_mode    SMALLINT     NOT NULL DEFAULT 0,
    extracted_at    TIMESTAMPTZ,
    extracted_count INTEGER      NOT NULL DEFAULT 0,
    deleted         BOOLEAN      NOT NULL DEFAULT FALSE,
    pinned          BOOLEAN      NOT NULL DEFAULT FALSE,
    taged           BOOLEAN      NOT NULL DEFAULT FALSE,
    create_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    update_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_sessions_user_id ON chat_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_sn      ON chat_sessions(sn);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_pinned  ON chat_sessions(pinned);

CREATE TRIGGER trg_chat_sessions_update_at
    BEFORE UPDATE ON chat_sessions
    FOR EACH ROW
    EXECUTE FUNCTION update_update_at_column();

-- ============================================================
-- 5. 对话消息表
--    用 chat_id (FK → chat_sessions.id) 做关联，性能最优
-- ============================================================

CREATE TABLE IF NOT EXISTS chat_messages (
    id           BIGSERIAL PRIMARY KEY,
    chat_id      BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    group_index  INTEGER      NOT NULL,
    role         SMALLINT     NOT NULL,                  -- 0=user, 1=assistant
    reasoning    TEXT,
    content      TEXT         NOT NULL,
    extracted    BOOLEAN      NOT NULL DEFAULT FALSE,
    interrupted  SMALLINT     NOT NULL DEFAULT 0,        -- 0=done, 1=interrupted, 2=error
    create_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    update_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_id ON chat_messages(chat_id);

CREATE TRIGGER trg_chat_messages_update_at
    BEFORE UPDATE ON chat_messages
    FOR EACH ROW
    EXECUTE FUNCTION update_update_at_column();

-- ============================================================
-- 6. 网络搜索来源表
-- ============================================================

CREATE TABLE IF NOT EXISTS web_sources (
    id           BIGSERIAL PRIMARY KEY,
    chat_id      BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    msg_id       BIGINT       NOT NULL,
    title        TEXT         NOT NULL DEFAULT '',
    content      TEXT         NOT NULL DEFAULT '',
    url          TEXT         NOT NULL DEFAULT '',
    site_name    TEXT         NOT NULL DEFAULT '',
    site_icon    TEXT         NOT NULL DEFAULT '',
    publish_date TEXT         NOT NULL DEFAULT '',
    score        REAL         NOT NULL DEFAULT 0,
    create_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_web_sources_chat_msg ON web_sources(chat_id, msg_id);

-- ============================================================
-- 7. 对话标签表
-- ============================================================

CREATE TABLE IF NOT EXISTS chat_tags (
    id        BIGSERIAL PRIMARY KEY,
    chat_id   BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    tag       TEXT         NOT NULL,
    create_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_tags_chat_id ON chat_tags(chat_id);
CREATE INDEX IF NOT EXISTS idx_chat_tags_tag     ON chat_tags(tag);

-- ============================================================
-- 8. 对话收藏表
-- ============================================================

CREATE TABLE IF NOT EXISTS chat_favorites (
    id         BIGSERIAL PRIMARY KEY,
    chat_id    BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    custom_tag TEXT         NOT NULL DEFAULT '',
    create_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    update_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_favorites_unique ON chat_favorites(chat_id, custom_tag);

CREATE TRIGGER trg_chat_favorites_update_at
    BEFORE UPDATE ON chat_favorites
    FOR EACH ROW
    EXECUTE FUNCTION update_update_at_column();

-- ============================================================
-- 9. 个人特征表（从 per-user brain.db 合并）
--    chat_sn 保留 TEXT 引用（可读性好，与 sn 字符串匹配）
-- ============================================================

CREATE TABLE IF NOT EXISTS traits (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    trait          TEXT         NOT NULL,
    category       INTEGER      NOT NULL,
    confidence     INTEGER      NOT NULL,
    half_life      INTEGER      NOT NULL,
    privacy_level  INTEGER      NOT NULL DEFAULT 0,
    chat_sn        TEXT         NOT NULL DEFAULT '',      -- 引用 chat_sessions.sn（可读性）
    create_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    update_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_traits_user_id    ON traits(user_id);
CREATE INDEX IF NOT EXISTS idx_traits_category   ON traits(category);
CREATE INDEX IF NOT EXISTS idx_traits_half_life  ON traits(half_life);
CREATE INDEX IF NOT EXISTS idx_traits_create_at  ON traits(create_at);
CREATE INDEX IF NOT EXISTS idx_traits_chat_sn    ON traits(chat_sn);

CREATE TRIGGER trg_traits_update_at
    BEFORE UPDATE ON traits
    FOR EACH ROW
    EXECUTE FUNCTION update_update_at_column();

-- ============================================================
-- 10. 特征向量表（pgvector 替代 sqlite-vec）
-- ============================================================

CREATE TABLE IF NOT EXISTS trait_vectors (
    trait_id  BIGINT PRIMARY KEY REFERENCES traits(id) ON DELETE CASCADE,
    embedding VECTOR(2048)                               -- 维度与嵌入模型匹配
);

-- HNSW 索引（需要 pgvector >= 0.5.0）
-- cosine 距离，ef_construction=64 是精度/速度平衡的推荐值
CREATE INDEX IF NOT EXISTS idx_trait_vectors_hnsw
    ON trait_vectors USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- ============================================================
-- 11. 特征关键词表
-- ============================================================

CREATE TABLE IF NOT EXISTS keywords (
    id        BIGSERIAL PRIMARY KEY,
    word      TEXT         NOT NULL,
    kind      INTEGER      NOT NULL,                    -- 1=A, 2=B, 3=C, 4=D, 5=E, 6=F
    trait_id  BIGINT       NOT NULL REFERENCES traits(id) ON DELETE CASCADE,
    create_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keywords_trait_id      ON keywords(trait_id);
CREATE INDEX IF NOT EXISTS idx_keywords_word          ON keywords(word);
CREATE INDEX IF NOT EXISTS idx_keywords_kind          ON keywords(kind);
CREATE INDEX IF NOT EXISTS idx_keywords_trait_kind    ON keywords(trait_id, kind);

-- ============================================================
-- 12. 验证
-- ============================================================

-- 查看所有表
SELECT table_name FROM information_schema.tables
WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
ORDER BY table_name;

-- 查看 pgvector 扩展
SELECT extname, extversion FROM pg_extension WHERE extname = 'vector';
```
