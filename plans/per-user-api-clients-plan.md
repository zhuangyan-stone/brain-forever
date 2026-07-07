# 每用户独立 API 客户端 — 改造方案（v2）

## 1. 设计思路

**核心原则**：全局维护 Provider → Client 的注册表，所有客户端共享 Transport（连接池）。用户只存储自己的 API Key 配置，请求时通过 Provider 查找对应的 Client，再传入用户的 API Key。

```mermaid
flowchart TD
    subgraph 初始化阶段 InitAgent
        CFG[config 配置] --> LLM_MAP[llmClients map[provider]llm.Client]
        CFG --> EMB_MAP[embedderClients map[provider]embedder.Embedder]
        CFG --> SEARCH_MAP[webSearchClients map[provider]toolimp.WebSearcher]
    end

    subgraph 请求阶段
        SESSION[session.user.settings] --> LLM_KEY[APIKey.LLM: provider + apiKey]
        SESSION --> EMB_KEY[APIKey.Embedder: provider + apiKey]
        SESSION --> SEARCH_KEY[APIKey.Search: provider + apiKey]

        LLM_KEY --> LLM_CLIENT[从 llmClients 查 provider]
        LLM_CLIENT --> LLM_CALL[client.ChatWithOptions ctx, req, apiKey]

        EMB_KEY --> EMB_CLIENT[从 embedderClients 查 provider]
        EMB_CLIENT --> EMB_CALL[client.Embed ctx, text]

        SEARCH_KEY --> SEARCH_CLIENT[从 webSearchClients 查 provider]
        SEARCH_CLIENT --> SEARCH_CALL[client.SearchForLLM ctx, query, ...]
    end
```

## 2. 当前状态（已完成的 LLM 改造）

### 2.1 infra 层已改造

- [`llm.Client`](infra/llm/client.go) 接口所有方法新增 `apiKey string` 参数
- [`DeepSeekClient`](infra/llm/deepseek.go) 去除 `os.Getenv`，请求时用传入的 `apiKey`

### 2.2 Agent 层已改造

- [`sessionUser`](internal/agent/session.go) 新增 `settings store.UserSettings` 字段
- [`switchToUser`](internal/agent/session.go) 接受 settings 参数
- [`OnLogin`](internal/agent/on_login.go) 解析 `user.Settings` 并传入
- [`sessionLLMAPIKey`](internal/agent/on_chat.go) accessor
- 所有 LLM 调用点传入用户的 API Key

### 2.3 待改造的问题

当前 `ChatAgent` 仍然持有 **单个** `charLLMClient` 字段，而不是 provider 注册表。注册表应该是模块级单例，不绑定到 ChatAgent。

## 3. Provider 注册表方案

### 3.1 定义 Provider 常量

在 [`internal/agent/init.go`](internal/agent/init.go) 或独立文件定义：

```go
const (
    ProviderDeepSeek = "deepseek"
    ProviderAli      = "ali"      // embedder
    ProviderZhipu    = "zhipu"    // embedder + searcher
    ProviderBocha    = "bocha"    // searcher
)
```

### 3.2 Provider 注册表 — 模块级单例

三个客户端注册表定义为 [`init.go`](internal/agent/init.go) 的包级变量，`InitAgent` 中初始化，不放在 ChatAgent 中：

```go
// 包级变量 — 全局唯一，所有 ChatAgent 实例共享
var (
    llmClients       map[string]llm.Client           // provider → LLM client
    embedderClients  map[string]embedder.Embedder    // provider → Embedder
    webSearchClients map[string]toolimp.WebSearcher  // provider → WebSearcher
)
```

ChatAgent 不再保有 `charLLMClient`、`embedder`、`webSearcher` 字段：

```go
type ChatAgent struct {
    sessionManager *SessionManager
    cookieName     string
    defaultLang    string
    avatarDir      string
    logger         zylog.Logger
}
```

### 3.3 新增 Accessor 方法（包级函数）

Accessor 不再属于 ChatAgent，而是包级函数（因为注册表是包级变量）：

```go
// sessionLLMClient returns the LLM client for the user's configured provider.
// Falls back to a default provider if the user hasn't set one.
func sessionLLMClient(s *session) llm.Client {
    provider := s.user.settings.APIKey.LLM.Provider
    if provider == "" {
        provider = ProviderDeepSeek // default
    }
    if c, ok := llmClients[provider]; ok {
        return c
    }
    return llmClients[ProviderDeepSeek] // fallback
}

// sessionLLMAPIKey returns the user's personal LLM API key.
func sessionLLMAPIKey(s *session) string {
    return s.user.settings.APIKey.LLM.ApiKey
}

// 同理：
// sessionEmbedder(s *session) embedder.Embedder
// sessionEmbedderAPIKey(s *session) string
// sessionWebSearcher(s *session) toolimp.WebSearcher
// sessionWebSearchAPIKey(s *session) string
```

### 3.4 NewChatHandler 签名简化

不再需要传入客户端，ChatAgent 专注于 session 管理：

```go
func NewChatHandler(
    cookieName string,
    defaultLang string,
    avatarDir string,
    logger zylog.Logger,
) *ChatAgent
```

### 3.5 InitAgent 改造

```go
func InitAgent(ctx context.Context, cfg config.Config, cookieName string, defaultLang string, logger zylog.Logger) (*ChatAgent, error) {
    // 1. 初始化所有 LLM Client（包级变量）
    llmClients = make(map[string]llm.Client)
    llmClients[ProviderDeepSeek] = InitLLMClient(cfg.ChatLLM, logger)

    // 2. 初始化所有 Embedder（包级变量）
    embedderClients = make(map[string]embedder.Embedder)
    embedderClients[ProviderAli] = InitEmbedder(config.EmbedderConfig{
        Provider: ProviderAli,
        APIKey:   cfg.Embedder.APIKey,
        Dimension: cfg.Embedder.Dimension,
    }, logger)
    embedderClients[ProviderZhipu] = InitEmbedder(config.EmbedderConfig{
        Provider: ProviderZhipu,
        APIKey:   cfg.Embedder.APIKey,
        Dimension: cfg.Embedder.Dimension,
    }, logger)

    // 3. 初始化所有 Web Search Client（包级变量）
    webSearchClients = make(map[string]toolimp.WebSearcher)
    webSearchClients[ProviderBocha] = InitWebSearchClient(..., logger)
    webSearchClients[ProviderZhipu] = InitWebSearchClient(..., logger)

    // 4. 初始化 dbc（只需要 dimension，从默认 embedder 获取）
    defaultEmbedder := embedderClients[ProviderAli]
    dbc.InitDBConfig("localdb", defaultEmbedder.Dimension(), logger)

    // 5. 创建 ChatHandler（不再传客户端）
    chatHandler := NewChatHandler(
        cookieName,
        defaultLang,
        avatarDir,
        logger,
    )
    ...
}
```

### 3.6 所有调用点改造

将 `h.charLLMClient` → `sessionLLMClient(session)`（包级函数，无 `h.`），`h.embedder` → `sessionEmbedder(session)`，等。

| 文件 | 原代码 | 新代码 |
|------|--------|--------|
| [`chatllm.go`](internal/agent/chatllm.go:192) | `h.charLLMClient.ChatWithPipeline(...)` | `sessionLLMClient(session).ChatWithPipeline(..., apiKey)` |
| [`chatllm.go`](internal/agent/chatllm.go:225) | `h.charLLMClient.GetUsageInfo()` | `sessionLLMClient(session).GetUsageInfo()` |
| [`on_msg_new.go`](internal/agent/on_msg_new.go:161) | `h.webSearcher` | `sessionWebSearcher(session)` |
| [`on_msg_new.go`](internal/agent/on_msg_new.go:177) | `h.embedder` | `sessionEmbedder(session)` |
| [`on_traits.go`](internal/agent/on_traits.go:367) | `h.embedder` | `sessionEmbedder(session)` |
| [`on_traits.go`](internal/agent/on_traits.go:256/264) | `h.charLLMClient.Model()/ChatWithOptions()` | `sessionLLMClient(session).Model()/ChatWithOptions(..., apiKey)` |
| [`on_doc_title.go`](internal/agent/on_doc_title.go:83) | `h.charLLMClient.Chat(...)` | `sessionLLMClient(session).Chat(..., apiKey)` |
| [`on_title.go`](internal/agent/on_title.go:306) | `h.charLLMClient.Chat(...)` | `sessionLLMClient(session).Chat(..., apiKey)` |
| [`on_tag.go`](internal/agent/on_tag.go:197) | `h.charLLMClient.ChatWithOptions(...)` | `sessionLLMClient(session).ChatWithOptions(..., apiKey)` |
| [`on_portrait.go`](internal/agent/on_portrait.go:302-309) | `h.charLLMClient.Model()/ChatStreamWithOptions(...)` | `sessionLLMClient(session).Model()/ChatStreamWithOptions(..., apiKey)` |
| [`on_portrait.go`](internal/agent/on_portrait.go:323) | `h.charLLMClient.SetUsageInfo(...)` | `sessionLLMClient(session).SetUsageInfo(...)` |
| [`on_portrait.go`](internal/agent/on_portrait.go:354) | `h.charLLMClient` | `sessionLLMClient(session)` |
| [`on_chat.go`](internal/agent/on_chat.go:330-332) | `h.charLLMClient.Name()/Model()/Website()` | 见下 |
| [`on_chat.go`](internal/agent/on_chat.go:327) | `OnGetLLMInfo` | 需要 session 参数 |

### 3.7 OnGetLLMInfo 特殊处理

需要 session 来获取用户的 provider：

```go
func (h *ChatAgent) OnGetLLMInfo(w http.ResponseWriter, r *http.Request) {
    sessionID := h.resolveSessionID(w, r)
    session := h.sessionManager.GetOrCreate(sessionID)
    client := sessionLLMClient(session)  // 包级函数

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(LLMInfo{
        Name:    client.Name(),
        Model:   client.Model(),
        Website: client.Website(),
    })
}
```

### 3.8 Embedder API Key 按请求传入

与 LLM 不同，[`Embedder`](infra/embedder/client.go) 接口只有三个方法：

```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    Model() string
    Dimension() int
}
```

当前 embedder 的 API Key 在构造时绑定。需要改为与 LLM 类似的模式：

**方案 A**：给 `Embed` 方法加 `apiKey` 参数
```go
type Embedder interface {
    Embed(ctx context.Context, text string, apiKey string) ([]float32, error)
    Model() string
    Dimension() int
}
```

**方案 B**：Embedder 也使用 Provider 注册表，但 API Key 不变（因为 embedder 的 dimension 必须一致）
- 所有用户使用相同 provider + model 的 embedder（dimension 一致性要求）
- 除非用户使用完全相同的 model（不同 provider 但相同 dimension）

方案 B 更简单：embedder 的 API Key 取自 config，不按用户区分。

### 3.9 WebSearcher API Key 按请求传入

[`toolimp.WebSearcher`](internal/agent/toolimp/web_search.go) 接口：

```go
type WebSearcher interface {
    SearchForLLM(ctx context.Context, query string, freshness string, count int) (llmText string, webPages []WebSource, err error)
}
```

当前 [`webSearchAdapter`](internal/agent/web_searcher.go) 包装了 `searcher.WebSearcher`，API Key 在构造时绑定。

**方案 A**：给 `SearchForLLM` 加 `apiKey` 参数

**方案 B**：与 LLM 一致，Provider 注册表 + 每请求传 API Key

### 3.10 调用链分析

`callLLMWithPipeline` 当前签名：
```go
func (h *ChatAgent) callLLMWithPipeline(ctx context.Context, sseWriter *sse.Writer,
    userMsgID int64, messages []llm.Message, tools []llm.ToolIMP,
    withDeepThink bool, lang string, apiKey string) *Message
```

需要新增 `session *session` 参数以获取 provider 对应的 client：
```go
func (h *ChatAgent) callLLMWithPipeline(ctx context.Context, sseWriter *sse.Writer,
    userMsgID int64, messages []llm.Message, tools []llm.ToolIMP,
    withDeepThink bool, lang string, session *session) *Message {
    client := sessionLLMClient(session)  // 包级函数
    apiKey := sessionLLMAPIKey(session)
    // 使用 client 而不是 h.charLLMClient
    ...
}
```

### 3.11 Redis 持久化 — settings 也需要存入

当前 [`SetLoginSession`](internal/store/session_store.go:72) 只存 `user_id` 和 `user_sn`：

```go
rs.client.HSet(ctx, key,
    "user_id", strconv.FormatInt(userID, 10),
    "user_sn", userSN,
    "created_at", now,
    "last_active", now,
)
```

重启后恢复时 [`GetOrCreate`](internal/agent/session_mgr.go:86) 只恢复 `ID` 和 `SN`，`settings` 丢失：

```go
user: sessionUser{
    ID:          restoredID,
    SN:          restoredSN,
    currentChat: &chat{},
    // settings 为零值！
},
```

需要改造：

**3.11.1 [`SetLoginSession`](internal/store/session_store.go)** — 新增 `settingsJSON string` 参数：

```go
func (rs *RedisSessionStore) SetLoginSession(ctx context.Context, sessionID string, userID int64, userSN string, settingsJSON string) error {
    // ... 原有逻辑 + settings 字段
    rs.client.HSet(ctx, key,
        "user_id", ...,
        "user_sn", ...,
        "settings", settingsJSON,  // 新增
        "created_at", ...,
        "last_active", ...,
    )
}
```

**3.11.2 [`GetLoginSession`](internal/store/session_store.go)** — 返回 `settingsJSON`：

```go
func (rs *RedisSessionStore) GetLoginSession(ctx context.Context, sessionID string) (userID int64, userSN string, settingsJSON string, ok bool, err error) {
    // ... 读取 settings 字段
    settingsStr := data["settings"]
    return uid, snStr, settingsStr, true, nil
}
```

**3.11.3 [`OnLogin`](internal/agent/on_login.go)** — 传入 settings JSON：

```go
settingsJSON := userSettings.ToString()  // UserSettings → JSON string
h.sessionManager.redis.SetLoginSession(
    h.sessionManager.ctx, sessionID, user.ID, user.SN, settingsJSON,
)
```

**3.11.4 [`GetOrCreate`](internal/agent/session_mgr.go)** — 恢复 settings：

```go
var restoredSettings store.UserSettings
if settingsJSON != "" {
    restoredSettings.FromString(settingsJSON)
}
user: sessionUser{
    ID:       restoredID,
    SN:       restoredSN,
    settings: restoredSettings,  // 恢复！
    currentChat: &chat{},
},
```

## 4. 实施步骤

### Phase 1: LLM Provider 注册表（已完成 LLM 接口改造）

| 步骤 | 文件 | 内容 |
|------|------|------|
| 1.1 | [`infra/llm/client.go`](infra/llm/client.go) | ✅ 接口加 `apiKey` 参数 |
| 1.2 | [`infra/llm/deepseek.go`](infra/llm/deepseek.go) | ✅ 去除 `os.Getenv`，使用传入 `apiKey` |
| 1.3 | [`internal/agent/session.go`](internal/agent/session.go) | ✅ `sessionUser.settings` |
| 1.4 | [`internal/agent/on_login.go`](internal/agent/on_login.go) | ✅ 解析 settings |
| 1.5 | 多个 handler | ✅ 传入 `apiKey` |

### Phase 2: 注册表架构迁移（LLM 部分先行）

| 步骤 | 文件 | 内容 |
|------|------|------|
| 2.1 | [`internal/agent/init.go`](internal/agent/init.go) | 定义 Provider 常量 + 包级 `llmClients` 变量 |
| 2.2 | [`internal/agent/on_chat.go`](internal/agent/on_chat.go) | ChatAgent 移除三个客户端字段；`NewChatHandler` 简化签名 |
| 2.3 | [`internal/agent/on_chat.go`](internal/agent/on_chat.go) | 新增包级 `sessionLLMClient()` accessor |
| 2.4 | [`internal/agent/on_chat.go`](internal/agent/on_chat.go) | 改造 `OnGetLLMInfo` 使用 session |
| 2.5 | [`internal/agent/init.go`](internal/agent/init.go) | `InitAgent` 初始化包级 `llmClients`，改造 `NewChatHandler` 调用 |

### Phase 3: 调用点全面改造

| 步骤 | 文件 | 内容 |
|------|------|------|
| 3.1 | [`internal/agent/chatllm.go`](internal/agent/chatllm.go) | `h.charLLMClient` → `sessionLLMClient(session)`，`callLLMWithPipeline` 接收 `session` |
| 3.2 | [`internal/agent/on_msg_new.go`](internal/agent/on_msg_new.go) | `h.webSearcher` → `sessionWebSearcher(session)`，`h.embedder` → `sessionEmbedder(session)` |
| 3.3 | [`internal/agent/on_traits.go`](internal/agent/on_traits.go) | `h.embedder` → `sessionEmbedder(session)`，`h.charLLMClient` → `sessionLLMClient(session)` |
| 3.4 | [`internal/agent/on_doc_title.go`](internal/agent/on_doc_title.go) | `h.charLLMClient` → `sessionLLMClient(session)` |
| 3.5 | [`internal/agent/on_title.go`](internal/agent/on_title.go) | 同上 |
| 3.6 | [`internal/agent/on_tag.go`](internal/agent/on_tag.go) | 同上 |
| 3.7 | [`internal/agent/on_portrait.go`](internal/agent/on_portrait.go) | 同上 |

### Phase 4: Embedder 接口改造

| 步骤 | 文件 | 内容 |
|------|------|------|
| 4.1 | [`infra/embedder/client.go`](infra/embedder/client.go) | `Embed` 方法加 `apiKey` 参数 |
| 4.2 | [`infra/embedder/ali.go`](infra/embedder/ali.go) | 实现改造，去除 `os.Getenv` |
| 4.3 | [`infra/embedder/zhipu.go`](infra/embedder/zhipu.go) | 同上 |
| 4.4 | [`internal/agent/init.go`](internal/agent/init.go) | 构建 `embedderClients` map |

### Phase 5: WebSearcher 接口改造

| 步骤 | 文件 | 内容 |
|------|------|------|
| 5.1 | [`infra/searcher/client.go`](infra/searcher/client.go) | `WebSearcher` 接口 `Search`/`SearchForLLM` 加 `apiKey` 参数 |
| 5.2 | [`infra/searcher/bocha.go`](infra/searcher/bocha.go) | 实现改造 |
| 5.3 | [`infra/searcher/zhipu.go`](infra/searcher/zhipu.go) | 实现改造 |
| 5.4 | [`internal/agent/web_searcher.go`](internal/agent/web_searcher.go) | `webSearchAdapter` 透传 apiKey |
| 5.5 | [`internal/agent/toolimp/web_search.go`](internal/agent/toolimp/web_search.go) | `MakeWebSearchTool` 接收 apiKey |
| 5.6 | [`internal/agent/init.go`](internal/agent/init.go) | 构建 `webSearchClients` map |

### Phase 6: 编译验证 + 清理

| 步骤 | 内容 |
|------|------|
| 6.1 | `go build ./cmd/server/` |
| 6.2 | 检查 `os.Getenv` 是否从 infra 层全部移除 |
| 6.3 | 更新计划文档 |

## 5. Embedder Dimension 一致性说明

所有用户必须使用**相同 dimension** 的 embedder，因为向量数据库的索引维度是固定的。

即使使用不同 API Key，也必须使用相同 model（相同 dimension）。

建议：embedder 的 provider/model/dimension 由服务器配置决定，用户只能替换 API Key。

## 6. 匿名用户处理

匿名用户 `session.user.settings` 为零值，accessor 会回退到默认 provider 的默认客户端（使用 config 的 API Key）。
