# OnNewMessage 锁粒度改进方案的严谨分析

> 基于 `plans/concurrency-lock-analysis.md` 第 201 行的讨论，补充更严谨的分析和可执行方案。
> 阅读前请确保已阅读原文档。

## 一、锁层级确认

`OnNewMessage` 使用的是 **`session.mu`**（[`internal/agent/types.go:102`](internal/agent/types.go:102)），**与 `SessionManager.mu` 无关**。

| 锁 | 类型 | 保护范围 | OnNewMessage 中的持有方式 |
|---|------|---------|--------------------------|
| `SessionManager.mu` | `sync.RWMutex` | `sessions map[string]*session` | 仅在 `GetOrCreate` 中微秒级持有，用于查找/创建 session |
| `session.mu` | `sync.Mutex` | session 内部所有字段（`currentChat`, `chats`, `chatStore` 等） | **`Lock()` 在 [`line 285`](internal/agent/on_chat.go:285)，`Unlock()` 在 `defer` —— 直到 handler 返回** |

`SessionManager.mu` 在 [`GetOrCreate`](internal/agent/types.go:260) 中短暂持有（先 `RLock` 读 map，不存在时 `Lock` 写 map），返回 session 指针后立即释放。**`OnNewMessage` 的问题确在 `session.mu`，与 `SessionManager.mu` 无关。**

## 二、根本问题：appendNewResponseMessage 被嵌入在 callLLMWithPipeline 内部

当前调用链路：

```
OnNewMessage (on_chat.go:274)
  ├── session.mu.Lock()                          ← 285行，开始持锁
  ├── appendNewRequestMessage(session, ...)       ← 需要锁：写 history
  ├── toRawMessages(session.getAllHistoryWithoutLock())  ← 需要锁：读 history
  ├── callLLMWithPipeline(ctx, session, ...)      ← 内部包含历史写入！
  │     ├── charLLMClient.ChatWithPipeline(...)   ← 10-60秒，不需要锁
  │     └── appendNewResponseMessage(session, ...) ← 需要锁：写 history
  └── defer session.mu.Unlock()                   ← handler 返回才释放
```

[`callLLMWithPipeline`](internal/agent/chat_callm.go:172) 的第 245 行调用了 `appendNewResponseMessage(session, &assistantMsg)` —— 这是 **整个流式调用期间必须持锁的根本原因**。

## 三、严谨改进方案

### 3.1 核心思路

将 `callLLMWithPipeline` 从 `session` 状态中**解耦**：让它不接收 `session` 参数，不操作任何 session 状态，只返回 `assistantMsg` 数据。由 `OnNewMessage` 在锁保护下完成最终的 history 写入。

### 3.2 涉及的文件和变更

#### 文件 1：[`internal/agent/types.go`](internal/agent/types.go)

在 `chat` 结构体中新增 `generation` 计数器，用于检测 Phase 2 无锁期间是否有其他 goroutine 并发修改了 history。

```go
type chat struct {
    history    []Message
    generation int64              // ++ 新增：每次修改 history 时递增
    title      string
    titleState TitleState
    dbSessionID int64
}

// 新增方法
func (c *chat) incrementGenerationWithoutLock() {
    c.generation++
}

func (s *session) getGenerationWithoutLock() int64 {
    if s.currentChat == nil {
        return 0
    }
    return s.currentChat.generation
}
```

在以下方法的 `WithoutLock` 变体中调用 `incrementGenerationWithoutLock`：

- [`appendHistoryWithoutLock`](internal/agent/types.go:164) —— 追加消息时递增
- [`deleteHistoryRangeWithoutLock`](internal/agent/types.go:171) —— 删除消息时递增

#### 文件 2：[`internal/agent/chat_callm.go`](internal/agent/chat_callm.go)

**重构 `callLLMWithPipeline`**：

```go
// 重构前：接收 session，内部调用 appendNewResponseMessage
func (h *ChatAgent) callLLMWithPipeline(
    ctx context.Context,
    session *session,          // ← 移除
    sseWriter *sse.Writer,
    userMsgID int64,
    messages []llm.Message,
    tools []llm.ToolIMP,
    withDeepThink bool,
    lang string,
) {
    // ... ChatWithPipeline ...
    appendNewResponseMessage(session, &assistantMsg)  // ← 移除
}

// 重构后：不接收 session，返回 assistantMsg
func (h *ChatAgent) callLLMWithPipeline(
    ctx context.Context,
    sseWriter *sse.Writer,
    userMsgID int64,
    messages []llm.Message,
    tools []llm.ToolIMP,
    withDeepThink bool,
    lang string,
) (assistantMsg *Message, usage *Usage) {
    // ... ChatWithPipeline ...

    if len(reply) > 0 {
        assistantMsg = &Message{
            ID:        userMsgID,
            Role:      llm.RoleAssistant,
            Content:   reply,
            Reasoning: reasoning,
            CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
            Usage:     usage,
        }
        // 附加 web search sources
        webPages := pipeline.GetWebSearchResult()
        if len(webPages) > 0 {
            assistantMsg.Sources = webPages
        }
    }
    return assistantMsg, usage
}
```

#### 文件 3：[`internal/agent/on_chat.go`](internal/agent/on_chat.go)

**重构 `OnNewMessage` 为三段式加解锁**：

```go
func (h *ChatAgent) OnNewMessage(w http.ResponseWriter, r *http.Request) {
    req := h.resolveNewMessageRequest(w, r)
    if req == nil {
        return
    }

    sessionID := h.resolveSessionID(w, r)
    session := h.sessionManager.GetOrCreate(sessionID)

    // ==============================================
    // 阶段 1：加锁，追加用户消息，读取历史快照（微秒级）
    // ==============================================
    session.mu.Lock()

    lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
    if lang == "" {
        lang = h.defaultLang
    }

    appendNewRequestMessage(session, &req.Message, lang)

    if req.Message.ID <= 0 {
        panic("new message's ID is zero still after append to history")
    }

    // 必须深拷贝！不能返回原始切片引用
    historySnapshot := session.copyHistoryWithoutLock()
    generation := session.getGenerationWithoutLock()
    dbSessionID := session.getDbSessionIDWithoutLock()
    chatStore := session.chatStore

    session.mu.Unlock()
    // ==============================================

    // ==============================================
    // 阶段 2：无锁，构建 LLM 消息 + 流式调用（10-60 秒）
    // ==============================================
    llmMsgs := toRawMessages(historySnapshot)

    startSystemMsg := llm.Message{
        Role:    llm.RoleSystem,
        Content: makeFixSystemPromptContent(lang),
    }
    messages := make([]llm.Message, 0, 1+len(llmMsgs))
    messages = append(messages, startSystemMsg)
    messages = append(messages, llmMsgs...)

    timeQueryToolImp := toolimp.MakeTimeQueryTool(lang)
    toolsImp := []llm.ToolIMP{timeQueryToolImp}
    if req.WebSearchEnabled {
        webSearchToolImp := toolimp.MakeWebSearchTool(r.Context(), h.webSearcher, lang)
        toolsImp = append(toolsImp, webSearchToolImp)
    }

    sseWriter := sse.NewSSEWriter(w)

    // 注意：callLLMWithPipeline 不再接收 session 参数
    assistantMsg, usage := h.callLLMWithPipeline(
        r.Context(),
        sseWriter,
        req.Message.ID,
        messages,
        toolsImp,
        req.DeepThink,
        lang)
    // ==============================================

    // ==============================================
    // 阶段 3：重新加锁，追加 AI 回复（微秒级）
    // ==============================================
    session.mu.Lock()

    if assistantMsg != nil {
        // 冲突检测：generation 是否在 Phase 2 期间被修改
        if session.getGenerationWithoutLock() != generation {
            log.Printf("[WARN] history generation changed during LLM call: "+
                "expected %d, got %d. Concurrent modification detected (e.g., OnDeleteMessage from another tab).",
                generation, session.getGenerationWithoutLock())
        }

        appendNewResponseMessage(session, assistantMsg)
    }

    session.mu.Unlock()
    // ==============================================

    // SSE done 事件（无需锁）
    createdAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")
    sseWriter.WriteEvent(SSEEvent{
        Type:      "done",
        Usage:     usage,
        MsgID:     req.Message.ID,
        CreatedAt: createdAt,
    })
}
```

### 3.3 为什么必须深拷贝 historySnapshot

[`getAllHistoryWithoutLock`](internal/agent/types.go:189) 返回的是原始切片的引用：

```go
func (s *session) getAllHistoryWithoutLock() []Message {
    if s.currentChat == nil {
        return nil
    }
    return s.currentChat.history  // 返回原始引用！
}
```

如果在 Phase 2 无锁期间，另一个 goroutine（如 `OnDeleteMessage`）调用了 `deleteHistoryRangeWithoutLock`，会修改底层数组。而 `toRawMessages` 在 Phase 2 遍历这个切片时，可能读到不一致的数据（已被删除的消息部分残留），甚至更糟——如果底层数组被重新分配，会出现数据竞争。

**解决方案**：使用已存在的 [`copyHistoryWithoutLock`](internal/agent/types.go:178) 进行深拷贝：

```go
func (s *session) copyHistoryWithoutLock() []Message {
    if s.currentChat == nil {
        return nil
    }
    cp := make([]Message, len(s.currentChat.history))
    copy(cp, s.currentChat.history)
    return cp
}
```

### 3.4 generation 冲突检测策略

| 场景 | 检测结果 | 处理方式 |
|------|---------|---------|
| Phase 2 期间无并发写入 | generation 不变 | 正常追加，无日志 |
| Phase 2 期间有其他 Tab 发消息 | generation 增加 | 记录 WARN，仍追加回复（回复已生成，丢弃浪费用户时间） |
| Phase 2 期间用户删除消息 | generation 增加 | 同上 |
| Phase 2 期间页面刷新（只读） | generation 不变 | 正常追加 |

**设计决策**：检测到冲突时**不阻止追加**。原因：

1. AI 回复已经生成完成，用户等到了完整结果
2. 丢弃回复会浪费 10-60 秒的计算时间和 token 费用
3. 前端刷新页面后会从 DB 加载完整状态，看到最终一致的结果
4. WARN 日志足够用于调试和监控

## 四、与 Section 8（前端防护屏障）的关系

Section 8 提到前端 `isStreaming` 提供了防护，但那是**同 Tab 场景**的防护。改进方案的收益在于：

| 场景 | 改进前 | 改进后 |
|------|-------|-------|
| 同 Tab 页面刷新 | GET /api/session 被阻塞 10-60s | **不会被阻塞**（锁仅微秒级） |
| 同 Tab 其他操作 | UI 级别已拦截 | UI 级别已拦截 |
| 另一 Tab 操作当前 session | 被 `session.mu` 阻塞 | **不会被阻塞**（锁仅微秒级） |
| 侧栏重命名其他会话 | 被 `session.mu` 阻塞（无数据冲突） | **不会被阻塞** |

## 五、改进行动清单

按文件分组，含具体修改：

### types.go

1. `chat` 结构体新增 `generation int64` 字段
2. `appendHistoryWithoutLock` 中调用 `currentChat.incrementGenerationWithoutLock()`
3. `deleteHistoryRangeWithoutLock` 中调用 `currentChat.incrementGenerationWithoutLock()`
4. 新增 `getGenerationWithoutLock()` 方法

### chat_callm.go

5. `callLLMWithPipeline` 签名移除 `session *session` 参数
6. `callLLMWithPipeline` 移除内部的 `appendNewResponseMessage` 调用
7. `callLLMWithPipeline` 返回 `assistantMsg *Message` 和 `usage *Usage`
8. `callLLMWithPipeline` 内部构建并返回 assistantMsg（含 Sources 等字段）

### on_chat.go

9. `OnNewMessage` 拆分为三段式：阶段 1（锁内：append + 深拷贝快照 + 读 generation）
10. 阶段 2（无锁：构建 messages + tools + 调 callLLMWithPipeline）
11. 阶段 3（锁内：冲突检测 + appendNewResponseMessage）
12. 将 phase 2 中 lang 解析放到 phase 1 锁内（安全微秒级操作）
13. 将 `sseWriter.WriteEvent(SSEEvent{Type: "done", ...})` 移出锁外（无需锁）
