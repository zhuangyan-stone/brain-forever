# Excerpt 定时生成任务 — 实施文档

## 1. 设计概览

### 1.1 流程

```mermaid
flowchart TD
    Start([定时触发]) --> TW{检查时间窗口\nIsAllowedTimePoint}
    TW -->|不在窗口| Skip[跳过本次]
    TW -->|在窗口| Q[查询待处理对话\nListChatsPendingExcerpt]
    Q --> C{有未处理对话？}
    C -->|无| Done[完成]
    C -->|有| Loop[遍历每个对话]
    
    subgraph ProcessChat [单对话处理]
        P1[解析 UserSettings\n获取 lang / API key]
        P2[获取对话全部消息\nchatStore.ListMessages]
        P3[构建 LLM 消息\n消息前加 [msg_id] 编号]
        P4[调用 LLM\nForceToolChoice trip_excerpts]
        P5[解析 ToolCall 结果]
        P6{有摘录?}
        P6 -->|是| P7[转换 value_types -> IDs\n批量事务入库]
        P6 -->|否| P8[仅更新进度]
        P7 --> P8
        P8[UpsertExcerptProgress]
    end
    
    Loop --> ProcessChat --> C
```

### 1.2 关键设计决策

| 决策 | 方案 | 理由 |
|------|------|------|
| 进度追踪 | 独立表 `excerpt_progress` | 解耦 chat_sessions，便于未来扩展其他操作 |
| 查询条件 | `processed_at IS NULL OR processed_at < update_at - delayHours` | 避免对话一更新就反复处理，默认延迟 24h |
| 输入粒度 | 每次处理对话的**全部**消息 | LLM 需要完整上下文判断摘录价值 |
| 存储方式 | 一次性 `BatchInsertExcerpts`（事务） | 原子性保障 |
| LLM 接口 | ToolCall `trip_excerpts`（`ForceToolChoice`） | 结构化输出，无需解析 JSON 文本 |
| 值类型映射 | `ExcerptValueDictCache` 全局缓存 | 14 个固定值类型，只读，无需每次查 DB |

---

## 2. 文件变更清单

### 2.1 修改的文件

| # | 文件 | 变更内容 |
|---|------|----------|
| 1 | [`internal/config/config.go`](internal/config/config.go:499) | `ExcerptTaskConfig` 新增 `ExtractDelayHours int` 字段（默认 24h），参考 trait 配置命名 `extract_delay_hours` |
| 2 | [`infra/i18n/tlfile.go`](infra/i18n/tlfile.go:95) | 注册 `"trip_excerpts"` 到全局 `Tools` 列表 |
| 3 | [`internal/store/excerpts.go`](internal/store/excerpts.go:317) | 新增 `ChatPendingExcerpt`、`ListChatsPendingExcerpt`、`UpsertExcerptProgress`；`Excerpt`/`ExcerptInsertion` 新增 `MsgTime` 字段；6 处 SQL 同步更新 |
| 4 | [`internal/tasks/excerpt_job.go`](internal/tasks/excerpt_job.go) | 完整覆写 — 实现完整任务逻辑；依据 LLM 返回的 `msg_id` 倒查消息 `CreateAt` 填充 `MsgTime` |
| 5 | [`bin.template/.../002.excerpts.template.sql`](bin.template/settings_template/init_sql.template/002.excerpts.template.sql:39) | `excerpts` 表新增 `msg_time TIMESTAMPTZ` 列；新增复合索引 `idx_excerpts_user_msg_time(user_id, msg_time DESC)`；新增 `excerpt_progress` DDL |

### 2.2 新建的文件

| # | 文件 | 说明 |
|---|------|------|
| 1 | [`internal/agent/toolimp/trip_excerpts.go`](internal/agent/toolimp/trip_excerpts.go) | LLM ToolCall 工具定义 |
| 2 | [`internal/agent/on_excerpts.go`](internal/agent/on_excerpts.go) | 摘录消息构建 + LLM 调用逻辑 |
| 3 | [`lang/zh-CN/tools/trip_excerpts.toml`](lang/zh-CN/tools/trip_excerpts.toml) | 中文 i18n 资源 |
| 4 | [`lang/en/tools/trip_excerpts.toml`](lang/en/tools/trip_excerpts.toml) | 英文 i18n 资源 |

---

## 3. 各层实现详情

### 3.1 Config 层 — `ExcerptTaskConfig`

[`internal/config/config.go:491`](internal/config/config.go:491)

```go
type ExcerptTaskConfig struct {
    Enabled            bool         `toml:"enabled"`               // 默认 true
    IntervalSeconds    int          `toml:"interval_seconds"`      // 默认 86400（1次/天）
    ExtractDelayHours  int          `toml:"extract_delay_hours"`   // 默认 24（新增）
    BatchLimit         int          `toml:"batch_limit"`           // 默认 100
    AllowedWindows     [][]TimeOfDay `toml:"allowed_windows"`      // 默认空=全天
}
```

### 3.2 Tool 层 — `trip_excerpts`

[`internal/agent/toolimp/trip_excerpts.go`](internal/agent/toolimp/trip_excerpts.go)

参照 [`trip_traits.go`](internal/agent/toolimp/trip_traits.go) 模式实现。

**Tool 参数定义**（LLM 通过 ToolCall 输出）：

```json
{
  "excerpts": [
    {
      "excerpt_text": "做PPT像是在拼乐高……",
      "value_types": ["vent", "literary"],
      "context_summary": "用户吐槽改PPT时领导推翻基础框架",
      "reason": "用拼乐高比喻改PPT，生动形象",
      "msg_id": 99
    }
  ]
}
```

- `value_types` 的 enum 枚举值：`insight`, `humor`, `vent`, `methodology`, `rule`, `confession`, `nostalgia`, `regret`, `self_discovery`, `conviction`, `touching`, `deed`, `privacy`, `literary`
- 使用 `strict: true` 强制 LLM 输出符合 schema

### 3.3 Agent 层 — 消息构建与 LLM 调用

[`internal/agent/on_excerpts.go`](internal/agent/on_excerpts.go)

**`getExcerptSystemPrompt`**
- 从 i18n 加载 `[excerpt]` 节，替换 `{{.ChatTitle}}` 模板变量

**`buildExcerptLLMMessages`**
- System message：摘录系统提示词
- User/Assistant messages：每条前加 `[msg_id]` 编号（使用 [`chat_messages.id`](internal/store/messages.go:8)）
- 助手消息超过 1024 字时截断为头 500 + 尾 500 字

**`CallExcerptLLMStandalone`**（外部可调用入口）
- 接收 `title`, `dbMessages`, `lang`, `llm.Client`, `apiKey`
- 调用 `callExcerptLLMWithTool` 完成 LLM 通信

**`callExcerptLLMWithTool`**
- 创建 `TripExcerptsTool` 实例
- 构造请求：`Tools: [tripExcerptsTool]` + `ForceToolChoice("trip_excerpts")`
- 解析 `FinishReason == "tool_calls"` 的响应中的 ToolCall 参数
- 返回 `ExcerptResult{Excerpts []ExcerptItem}`

### 3.4 Store 层 — 数据结构与进度管理

[`internal/store/excerpts.go:317`](internal/store/excerpts.go:317)

**`Excerpt`** — excerpts 表行映射

```go
type Excerpt struct {
    ID             int64      `db:"id"`
    UserID         int64      `db:"user_id"`
    ChatID         int64      `db:"chat_id"`
    MsgID          int64      `db:"msg_id"`
    MsgTime        *time.Time `db:"msg_time"` // 来源消息的发送时间
    Values         []int16    `db:"values"`
    Content        string     `db:"content"`
    ContextSummary string     `db:"context_summary"`
    Reason         string     `db:"reason"`
    CreateAt       time.Time  `db:"create_at"`
}
```

**`ExcerptInsertion`** — 插入参数

```go
type ExcerptInsertion struct {
    UserID         int64
    ChatID         int64
    MsgID          int64
    MsgTime        *time.Time // 由 tasks 层根据 msg_id 倒查 messages 列表填充
    Values         []int16
    Content        string
    ContextSummary string
    Reason         string
}
```

**`ChatPendingExcerpt`** — JOIN 查询结果行

```go
type ChatPendingExcerpt struct {
    ID          int64      `db:"id"`
    UserID      int64      `db:"user_id"`
    Title       string     `db:"title"`
    ProcessedAt *time.Time `db:"processed_at"` // 来自 excerpt_progress
    UpdateAt    time.Time  `db:"update_at"`
    Settings    string     `db:"settings"`     // users.settings JSONB
}
```

**`ListChatsPendingExcerpt(delayHours, batchLimit)`**

```sql
SELECT cs.id, cs.user_id, cs.title, cs.update_at,
       cep.processed_at,
       u.settings
FROM chat_sessions cs
JOIN users u ON u.id = cs.user_id
LEFT JOIN excerpt_progress cep ON cep.chat_id = cs.id
WHERE cs.deleted = FALSE
  AND (cep.processed_at IS NULL
    OR cep.processed_at < cs.update_at - ($1::text || ' hours')::interval)
ORDER BY cs.update_at ASC
LIMIT $2
```

**`UpsertExcerptProgress(chatID)`**

```sql
INSERT INTO excerpt_progress(chat_id, processed_at)
VALUES($1, NOW())
ON CONFLICT (chat_id) DO UPDATE SET processed_at = NOW()
```

### 3.5 Tasks 层 — 任务运行器

[`internal/tasks/excerpt_job.go`](internal/tasks/excerpt_job.go)

**全局变量**
```go
var excerptVDCache *cache.ExcerptValueDictCache
```

**`RegisterPeriodicExcerptGeneration`** — 注册入口

签名扩展为：
```go
func RegisterPeriodicExcerptGeneration(
    cfg config.ExcerptTaskConfig,
    excerptStore *store.ExcerptStore,
    llmClients map[string]llm.Client,
    defaultLang string,
    vdCache *cache.ExcerptValueDictCache,
    logger zylog.Logger,
)
```

新增参数说明：
- `llmClients`：按 provider 名称索引的 LLM 客户端映射
- `defaultLang`：用户未设置语言时的降级值
- `vdCache`：摘录值类型缓存（存入全局变量）

**`runPeriodicExcerptGeneration`** — 主循环

1. 检查时间窗口（`IsAllowedTimePoint`）
2. 查询待处理对话（`ListChatsPendingExcerpt`）
3. 逐条处理（`processChatForExcerpt`）

**`processChatForExcerpt`** — 单对话处理流程

1. 解析 `UserSettings`，获取 `lang`、`llmProvider`、`llmAPIKey`
2. 调用 `chatStore.ListMessages(chatID)` 获取**全部**消息
3. 获取 LLM client（按 provider 查找）
4. 调用 `agent.CallExcerptLLMStandalone`
5. 如果返回摘录：建立 `msg_id → CreateAt` 映射（从 messages 列表倒查），`resolveValueTypeIDs` 转换 → 填入 `MsgTime` → `BatchInsertExcerpts`（事务）
6. 调用 `UpsertExcerptProgress` 标记完成

**`resolveValueTypeIDs`** — 值类型映射

```go
func resolveValueTypeIDs(valueTypes []string) []int16
```
通过全局 `excerptVDCache` 将 `["insight", "literary"]` 转换为 `[1, 14]`

### 3.6 DB Schema

[`bin.template/.../002.excerpts.template.sql:52`](bin.template/settings_template/init_sql.template/002.excerpts.template.sql:52)

```sql
CREATE TABLE IF NOT EXISTS excerpt_progress (
    chat_id      BIGINT       PRIMARY KEY REFERENCES chat_sessions(id) ON DELETE CASCADE,
    processed_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
```

---

## 4. 辅助函数 `IsAllowedTimePoint` 重构

[`internal/config/config.go:514`](internal/config/config.go:514)

`ExcerptTaskConfig.IsAllowedTimePoint` 和 `TraitExtractionTaskConfig.IsAllowedTimePoint` 之前有完全相同的实现。已提取为包内私有函数：

```go
func isAllowedTimePoint(windows [][]TimeOfDay, t time.Time) bool
```

两个方法都委托调用它：
```go
func (c *ExcerptTaskConfig) IsAllowedTimePoint(t time.Time) bool {
    return isAllowedTimePoint(c.AllowedWindows, t)
}
func (c *TraitExtractionTaskConfig) IsAllowedTimePoint(t time.Time) bool {
    return isAllowedTimePoint(c.AllowedWindows, t)
}
```

---

## 5. 服务器启动注册

需要在 [`cmd/server/main.go`](cmd/server/main.go) 或 [`cmd/server/routers.go`](cmd/server/routers.go) 中添加以下代码：

```go
// 创建 ExcerptStore
excerptStore := store.NewExcerptStore(logger)

// 加载摘录值类型缓存
excerptVDCache := cache.NewExcerptValueDictCache()
dicts, err := excerptStore.ListAllValueDicts()
if err == nil {
    excerptVDCache.Load(dicts)
}

// 注册定时任务
tasks.RegisterPeriodicExcerptGeneration(
    cfg.ExcerptTask,
    excerptStore,
    agent.GetLLMClients(),
    defaultLang,  // 服务器配置的默认语言
    excerptVDCache,
    logger,
)
```

---

## 6. 与 Trait Extraction 对比

| 维度 | Trait Extraction | Excerpt Generation |
|------|-----------------|-------------------|
| 进度字段 | `chat_sessions.extracted_at` | 独立表 `excerpt_progress` |
| 消息粒度 | 仅 `extracted=false` 的消息 | 对话的**全部**消息 |
| LLM 工具 | `trip_traits` | `trip_excerpts` |
| 输出结构 | `features[]` 含 category/keyword/halflife/privacy | `excerpts[]` 含 text/types/summary/reason |
| 存储方式 | `brainStore.AddTraits`（逐条） | `excerptStore.BatchInsertExcerpts`（事务） |
| 值映射 | 无需（trait 自带 category_id） | `resolveValueTypeIDs` 通过缓存转换 |
