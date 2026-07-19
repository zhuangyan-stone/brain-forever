# traits_job.go 实现计划

## 目标

在 [`internal/tasks/traits_job.go`](internal/tasks/) 中定义 job 函数，实现一个批次的 chat_session 个人特征抽取与存储。

## 方案

### 一、数据查询（JOIN 方式）

使用 `chat_sessions JOIN users` 一次性查出符合条件的记录及其用户 settings：

```sql
SELECT cs.id, cs.user_id, cs.title, cs.extracted_at, cs.extracted_count, cs.update_at,
       u.settings
FROM chat_sessions cs
JOIN users u ON u.id = cs.user_id
WHERE cs.deleted = FALSE
  AND (cs.extracted_at IS NULL OR cs.extracted_at < cs.update_at - ($1::text || ' hours')::interval)
ORDER BY cs.update_at ASC
LIMIT $2
```

结果结构体（**不需要 `sn`**，因为 `title` 已直接查出来，无需额外调用 `FindChatBySN`）：
```go
type chatWithUserRow struct {
    ID             int64      `db:"id"`
    UserID         int64      `db:"user_id"`
    Title          string     `db:"title"`
    ExtractedAt    *time.Time `db:"extracted_at"`
    ExtractedCount int        `db:"extracted_count"`
    UpdateAt       time.Time  `db:"update_at"`
    Settings       string     `db:"settings"` // users.settings JSONB
}
```

### 二、处理流程

每个 chat 的处理流程：

```
解析用户settings → 确定 lang/API keys → 加载未提取消息
    → 调用 LLM 提取特征 → 计算 embedding → 存储到 traits 表
    → 更新 extracted_at / extracted_count / messages.extracted
```

### 三、前端触发与后台任务的复用关系

这是设计的核心。前端触发的 `POST /api/chat/traits` 和后台定时任务需要执行相同的核心逻辑——**构建 LLM 消息 → 调用 LLM 的 trip_traits 工具 → 解析 tool call 结果 → 计算 embedding → 存储**。

为避免两份重复实现，采用**单一事实来源**设计：

```
                    ┌──────────────────────────────────┐
                    │  agent/on_traits.go              │
                    │                                  │
                    │  buildTraitsLLMMessages()         │ ← 包内共享：构建 LLM 消息列表
                    │  callTraitsLLMWithTool()          │ ← 包内共享：调用 LLM + 解析结果
                    │                                  │
                    │  CallTraitsLLMStandalone() ───────┤ ← 导出供 tasks 调用（无时间戳）
                    │  StoreTraitsStandalone()  ────────┤ ← 导出供 tasks 调用
                    └──────┬───────────────────┬────────┘
                           │                   │
              ┌────────────▼──────┐    ┌───────▼─────────────┐
              │ HTTP 前端触发      │    │ 后台定时任务          │
              │ agent/callTraitsLLM│    │ tasks/processChat... │
              │ (带时间戳)         │    │ (无时间戳)           │
              │ storeTraitsInSession│   │ agent.CallTraits...  │
              │ → StoreTraits...   │    │ agent.StoreTraits... │
              └───────────────────┘    └─────────────────────┘
```

#### 共享的函数

| 函数 | 位置 | 可见性 | 差异点 |
|------|------|--------|--------|
| `buildTraitsLLMMessages()` | `agent/on_traits.go` | 包内 | `withTimestamps` 参数控制是否添加 `[时间戳]` 前缀 |
| `callTraitsLLMWithTool()` | `agent/on_traits.go` | 包内 | 前后端完全一致 |
| `CallTraitsLLMStandalone()` | `agent/on_traits.go` | **导出** | 不带时间戳，供 `tasks` 调用 |
| `StoreTraitsStandalone()` | `agent/on_traits.go` | **导出** | 前后端完全一致 |

#### 差异说明

**前端触发**（`callTraitsLLM` 方法）在每条消息前添加 `[时间戳]` 前缀，为 LLM 提供时间上下文，帮助理解对话的时间线。

**后台任务**（`CallTraitsLLMStandalone`）不添加时间戳，因为后台处理的是历史对话，LLM 通过消息顺序即可理解上下文，时间戳并非必需。

两者在 LLM 请求构建（Tool choice、Model、Thinking 配置）、响应解析（tool call → `TripTraitsParams` → `TraitFeature` 列表）、embedding 计算和 traits 存储上**完全共享同一份代码**。

### 四、依赖传递方式

job 函数通过参数接收所有依赖（`tasks` 包不能直接访问 `agent` 包的全局变量）：

```go
func RegisterPeriodicTraitExtraction(
    cfg config.TraitExtractionTaskConfig,
    chatStore *store.ChatStore,
    brainStore *store.BrainStore,
    llmClients map[string]llm.Client,
    embedderClients map[string]embedder.Embedder,
    logger zylog.Logger,
    defaultLang string,
)
```

`agent` 包通过导出的 Getter 提供这些依赖：
- `agent.GetChatStore()`
- `agent.GetBrainStore()`
- `agent.GetLLMClients()`
- `agent.GetEmbedderClients()`

### 五、文件修改清单

| 文件 | 操作 |
|------|------|
| [`internal/tasks/traits_job.go`](internal/tasks/) | **新建** — 注册函数 + JOIN 查询 + 处理流水线 |
| [`internal/agent/on_traits.go`](internal/agent/on_traits.go) | 修改 — 添加 `buildTraitsLLMMessages()`、`callTraitsLLMWithTool()`、导出 `CallTraitsLLMStandalone()`、`StoreTraitsStandalone()` |
| [`internal/agent/chatdef.go`](internal/agent/chatdef.go) | 修改 — 添加 `GetChatStore()`、`GetBrainStore()`、`GetLLMClients()`、`GetEmbedderClients()` 导出 Getter |
| [`internal/store/chats.go`](internal/store/chats.go) | 修改 — 简化 `ListChatsPendingExtraction` SQL（去掉冗余的 `IS NOT NULL`） |
| [`cmd/server/main.go`](cmd/server/main.go) | 修改 — 调用 `tasks.RegisterPeriodicTraitExtraction()` 替代已删除的 `agent.RegisterPeriodicTraitExtraction()` |
| [`internal/agent/periodic_trait_extraction.go`](internal/agent/) | **删除** — 全部逻辑已合入 `tasks/traits_job.go` |

### 六、执行步骤

1. 在 `agent/on_traits.go` 中添加 `buildTraitsLLMMessages()`、`callTraitsLLMWithTool()` 包内共享函数，以及导出的 `CallTraitsLLMStandalone()`、`StoreTraitsStandalone()`
2. 重构 `agent/on_traits.go` 中现有的 `callTraitsLLM` 方法和 `storeTraitsInSession` 方法，使其委托给共享函数
3. 在 `agent/chatdef.go` 中添加导出 Getter
4. 在 `store/chats.go` 中简化 SQL
5. 新建 `internal/tasks/traits_job.go`，包含注册函数 + JOIN 查询 + 处理流水线（调用 `agent.CallTraitsLLMStandalone` 和 `agent.StoreTraitsStandalone`）
6. 删除 `internal/agent/periodic_trait_extraction.go`
7. 更新 `cmd/server/main.go` 中的注册调用
8. 全量编译验证
