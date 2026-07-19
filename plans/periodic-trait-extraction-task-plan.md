# 周期性个人特征提取后台任务方案（完整版）

## 1. 背景

当前系统已在对话过程中通过前端主动触发个人特征提取（`POST /api/chat/traits`）。但这种方式依赖前端在特定轮次后发起请求，可能存在遗漏。

需要添加一个**辅助的定时后台任务**，利用已有的 [`bktask`](../infra/bktask/bktask.go) 慢任务队列，定期扫描 [`chat_sessions`](../internal/store/chats.go:29) 表中需要重新提取（或首次提取）特征的对话，自动执行特征提取。

## 2. 前置条件：用户 Settings 增加 `lang` 字段

后台任务没有 HTTP 请求上下文，无法获取用户的 `Accept-Language`。因此需要在用户登录时，将语言偏好持久化到用户 settings 中。

### 2.1 修改点

**文件 A1：[`internal/store/user_settings.go`](../internal/store/user_settings.go)**

在 `UserSettings` 结构体增加 `Lang` 字段：

```go
type UserSettings struct {
    V      int                `json:"v"`
    Lang   string             `json:"lang"`    // 用户语言偏好，如 "zh-CN"（空=使用服务端默认）
    APIKey UserSettingsAPIKey `json:"api_key"`
    Theme  UserSettingsTheme  `json:"theme"`
}
```

`Init()` 中增加默认值：`s.Lang = ""`

**文件 A2：[`internal/user/login.go`](../internal/user/login.go)**

修改 [`afterLogin`](../internal/user/login.go:319) 函数：

1. 增加 `lang string` 参数
2. 若 `userSettings.Lang != lang`，更新 settings 并写回 DB
3. 调用方（`OnLoginBySMS` 和 `OnLoginByPwd`）从 `r.Header.Get("Accept-Language")` 提取 lang 传入

## 3. 核心逻辑：周期性特征提取

### 3.1 扫描条件

扫描 `chat_sessions` 表中满足以下任一条件的记录（`deleted = FALSE`）：

- **条件 A**：`extracted_at IS NULL`（从未提取过）
- **条件 B**：`extracted_at < update_at - N 小时`（N 可配置，默认 20h）

### 3.2 执行流程

```
定时器触发（默认每 1 小时）
    │
    ▼
查询符合条件的 chat_sessions（batchLimit=50）
    │
    ▼
对每个对话：
    ├─ 获取用户设置（含 Lang、API Keys）
    ├─ 查询未提取消息（ListUnExtractMessages）
    ├─ 无消息且 extracted_at 为空 → 直接更新 extracted_at
    ├─ 有未提取消息：
    │     ├─ 用用户 Lang 调用 LLM 提取特征
    │     ├─ 存储特征
    │     └─ 更新 extracted_at / extracted_count
    └─ 处理下一个...
```

### 3.3 API Key 优先级

用户私有 Key → 系统共享 Key → 跳过（日志警告）

### 3.4 语言选择

1. 优先使用 `userSettings.Lang`
2. 若为空，使用服务端 `defaultLang`

## 4. 影响文件清单

| # | 文件 | 操作 | 说明 |
|---|------|------|------|
| A1 | [`internal/store/user_settings.go`](../internal/store/user_settings.go) | 修改 | UserSettings 增加 Lang 字段 |
| A2 | [`internal/user/login.go`](../internal/user/login.go) | 修改 | afterLogin 接收 lang 参数并持久化 |
| B1 | [`internal/config/config.go`](../internal/config/config.go) | 修改 | 新增 TraitExtractionTaskConfig |
| B2 | [`internal/store/chats.go`](../internal/store/chats.go) | 修改 | 新增 ListChatsPendingExtraction |
| B3 | [`internal/agent/periodic_trait_extraction.go`](../internal/agent/) | **新建** | 后台任务核心逻辑 |
| B4 | [`cmd/server/main.go`](../cmd/server/main.go) | 修改 | 注册周期性任务 |
| B5 | [`bin.template/settings_template/server.template.toml`](../bin.template/settings_template/server.template.toml) | 修改 | 添加新配置项文档 |

## 5. 详细设计

### 5.1 配置结构体 — [`internal/config/config.go`](../internal/config/config.go)

```go
type TraitExtractionTaskConfig struct {
    Enabled            bool `toml:"enabled"`              // 默认 true
    IntervalSeconds    int  `toml:"interval_seconds"`     // 检查间隔（秒），默认 3600（1小时）
    ExtractDelayHours  int  `toml:"extract_delay_hours"`  // 重新提取阈值，默认 20
    BatchLimit         int  `toml:"batch_limit"`          // 每次最多处理 chat 数，默认 50
}
```

在 `Config` 中：
```go
type Config struct {
    // ... 现有字段 ...
    TraitExtractionTask TraitExtractionTaskConfig `toml:"trait-extraction-task"`
}
```

默认值：
```go
TraitExtractionTask: TraitExtractionTaskConfig{
    Enabled:           true,
    IntervalSeconds:   3600,
    ExtractDelayHours: 20,
    BatchLimit:        50,
},
```

### 5.2 数据库查询 — [`internal/store/chats.go`](../internal/store/chats.go)

```go
type ChatPendingExtraction struct {
    ID             int64      `db:"id"`
    UserID         int64      `db:"user_id"`
    SN             string     `db:"sn"`
    ExtractedAt    *time.Time `db:"extracted_at"`
    ExtractedCount int        `db:"extracted_count"`
    UpdateAt       time.Time  `db:"update_at"`
}

func (s *ChatStore) ListChatsPendingExtraction(delayHours int, batchLimit int) ([]ChatPendingExtraction, error) {
    // SQL: SELECT id, user_id, sn, extracted_at, extracted_count, update_at
    // FROM chat_sessions
    // WHERE deleted = FALSE
    //   AND (extracted_at IS NULL
    //        OR (extracted_at IS NOT NULL AND extracted_at < update_at - ($1 || ' hours')::interval))
    // ORDER BY update_at ASC
    // LIMIT $2
}
```

### 5.3 后台任务逻辑 — [`internal/agent/periodic_trait_extraction.go`](../internal/agent/)（新文件）

```go
package agent

func RegisterPeriodicTraitExtraction(cfg config.TraitExtractionTaskConfig, logger zylog.Logger, defaultLang string) {
    if !cfg.Enabled { return }
    interval := time.Duration(cfg.IntervalSeconds) * time.Second
    bktasks.Global().AddRecurring("periodic-trait-extraction", interval, func() error {
        return runPeriodicTraitExtraction(cfg.ExtractDelayHours, cfg.BatchLimit, logger, defaultLang)
    })
}

func runPeriodicTraitExtraction(delayHours, batchLimit int, logger zylog.Logger, defaultLang string) error {
    chats, err := theChatStore.ListChatsPendingExtraction(delayHours, batchLimit)
    if err != nil { return fmt.Errorf("query pending chats failed. %w", err) }
    for _, chat := range chats {
        processChatForExtraction(chat, logger, defaultLang)
    }
    return nil
}

func processChatForExtraction(chat store.ChatPendingExtraction, logger zylog.Logger, defaultLang string) {
    // 1. 获取用户设置
    userSettings, err := store.TheUserStore().GetUserSettings(chat.UserID)
    if err != nil {
        logger.Errorf("skip chat %d: get user settings failed. %v", chat.ID, err)
        return
    }
    
    // 2. 确定语言
    lang := userSettings.Lang
    if lang == "" { lang = defaultLang }
    
    // 3. 确定 API Keys
    llmProvider, llmAPIKey := resolveLLMConfig(userSettings)
    if llmAPIKey == "" {
        logger.Warnf("skip chat %d: no LLM API key", chat.ID)
        return
    }
    embedderProvider, embedderAPIKey := resolveEmbedderConfig(userSettings)
    if embedderAPIKey == "" {
        logger.Warnf("skip chat %d: no Embedder API key", chat.ID)
        return
    }
    
    // 4. 获取未提取消息
    messages, err := theChatStore.ListUnExtractMessages(chat.ID)
    if err != nil {
        logger.Errorf("skip chat %d: list messages failed. %v", chat.ID, err)
        return
    }
    if len(messages) == 0 {
        if chat.ExtractedAt == nil {
            theChatStore.UpdateExtractionCountAndTime(chat.ID, 0)
        }
        return
    }
    
    // 5. 查 chat title
    chatInfo, err := theChatStore.FindChatBySN(chat.SN)
    if err != nil {
        logger.Errorf("skip chat %d: find chat failed. %v", chat.ID, err)
        return
    }
    
    // 6. 调用 LLM 提取特征（复用现有 callTraitsLLM 逻辑的变体）
    llmClient := getLLMClientFromProvider(llmProvider)
    embedderClient := getEmbedderClientFromProvider(embedderProvider)
    llmAPIKeyActual := llmAPIKey  // 实际的 key（用户私有或系统共享）
    
    result := callTraitsLLMForTask(context.Background(), chatInfo.Title, messages, lang, llmClient, llmAPIKeyActual)
    if result == nil { return }
    
    // 7. 存储特征
    if len(result.Features) > 0 {
        insertions := buildTraitInsertions(result.Features, chat.ID, embedderClient, embedderAPIKey, chat.UserID)
        if len(insertions) > 0 {
            storedCount, err := theBrainStore.AddTraits(context.Background(), insertions)
            if err != nil {
                logger.Errorf("store traits for chat %d failed. %v", chat.ID, err)
                return
            }
            lastMsgID := messages[len(messages)-1].ID
            theChatStore.UpdateMessagesExtracted(chat.ID, lastMsgID, true)
            theChatStore.UpdateExtractionCountAndTime(chat.ID, storedCount)
            return
        }
    }
    // 无特征也标记已处理
    lastMsgID := messages[len(messages)-1].ID
    theChatStore.UpdateMessagesExtracted(chat.ID, lastMsgID, true)
    theChatStore.UpdateExtractionCountAndTime(chat.ID, 0)
}
```

### 5.4 注册任务 — [`cmd/server/main.go`](../cmd/server/main.go)

```go
// 在 bktasks.InitGlobal() 之后
agent.RegisterPeriodicTraitExtraction(cfg.TraitExtractionTask, theLogger, defaultLang)
```

## 6. 设计要点

- **避免重复**：每处理完一个 chat 立即更新 `extracted_at`
- **错误隔离**：单个 chat 异常不影响其他 chat（`processChatForExtraction` 内部 recover）
- **并发控制**：由 bktask 的 `WorkerCount` 限制
- **与前端互补**：前端提取本轮新增消息，后台抓漏补缺
- **匿名用户**：user_id=0 的 chat 不处理（因为无法获取 API 设置）
