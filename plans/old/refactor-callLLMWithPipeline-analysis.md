# callLLMWithPipeline 重构分析：去除 session 参数，由调用方 appendNewResponseMessage

## 当前调用链

### `OnNewMessage` ([`internal/agent/on_chat.go:274`](internal/agent/on_chat.go:274))

```
session.mu.Lock()                    ← 行 285
  appendNewRequestMessage()          ← 行 296: 用户消息入 history + DB
  callLLMWithPipeline(ctx, session, sseWriter, ...)  ← 行 328
    └─ ChatWithPipeline()            ← 行 186: 流式 LLM 调用（长时间）
    └─ 成功时:
         appendNewResponseMessage()  ← 行 245: assistant 消息入 history + DB
         pipeline.OnWebSource()      ← 行 247: SSE 写 sources 事件
    └─ sseWriter.WriteEvent(done)    ← 行 252: SSE 写 done 事件
session.mu.Unlock()                  ← defer 行 286
```

## 重构方案

将 `callLLMWithPipeline` 的签名从：

```go
func (h *ChatAgent) callLLMWithPipeline(
    ctx context.Context,
    session *session,        // ← 去除
    sseWriter *sse.Writer,
    userMsgID int64,
    messages []llm.Message,
    tools []llm.ToolIMP,
    withDeepThink bool,
    lang string,
)
```

改为：

```go
func (h *ChatAgent) callLLMWithPipeline(
    ctx context.Context,
    sseWriter *sse.Writer,
    userMsgID int64,
    messages []llm.Message,
    tools []llm.ToolIMP,
    withDeepThink bool,
    lang string,
) *Message   // ← 返回 assistantMsg
```

调用方逻辑变为：

```go
assistantMsg := h.callLLMWithPipeline(r.Context(), sseWriter, ...)
if assistantMsg != nil {
    appendNewResponseMessage(session, assistantMsg)  // ← 调用方执行
}
```

## 执行顺序变更

| 步骤 | 当前顺序 | 新顺序 |
|------|---------|--------|
| 1 | LLM streaming | LLM streaming |
| 2 | **appendNewResponseMessage** → history + DB | pipeline.OnWebSource → SSE sources |
| 3 | pipeline.OnWebSource → SSE sources | sseWriter.WriteEvent(done) |
| 4 | sseWriter.WriteEvent(done) | **appendNewResponseMessage** → history + DB |

## 边界效应逐项分析

### 1. 🟢 前端行为 — 无影响

[`frontend/static/chat-sse.js:56`](frontend/static/chat-sse.js:56) `handleDoneEvent` 分析：

- 行 70: `contentDiv.innerHTML = renderMarkdown(...)` — 最终渲染
- 行 92: `state.messages.push({...})` — **仅更新前端本地 state**
- 行 132-135: `autoScrollToBottom()` + `setTimeout` — 滚动
- ❌ **无任何后端轮询/刷新操作**

前端收到 `done` 后不会去验证后端 session history 的状态，所以 `appendNewResponseMessage` 在 `done` 之前还是之后执行，对前端无感知差异。

### 2. 🟢 并发安全 — 无变化

[`internal/agent/on_chat.go:285-286`](internal/agent/on_chat.go:285)：

```go
session.mu.Lock()
defer session.mu.Unlock()
```

整个 `callLLMWithPipeline` **在 `session.mu` 锁内执行**。重构后 `appendNewResponseMessage` 也在此锁内执行。同一 session 的并发请求被序列化，不会出现 data race。

### 3. 🟢 DB 持久化失败 — 无差异

[`internal/agent/on_chat.go:147`](internal/agent/on_chat.go:147) `persistMessageToDB` 在失败时仅 log error，不 panic：

```go
func persistMessageToDB(session *session, msg *Message) {
    ...
    if err := session.chatStore.InsertMessage(...); err != nil {
        log.Printf("failed to persist message to DB...")
    }
}
```

所以无论 `appendNewResponseMessage` 在 SSE done 之前还是之后执行，DB 写失败都不会影响前端 SSE 事件流。

### 4. 🟢 LLM 调用失败 — 无差异

当前代码在 `err != nil` 时：
- 行 192: `pipeline.OnError(err)` → SSE error 事件
- 不执行 `appendNewResponseMessage`
- 行 252: 仍然发送 `done` 事件（`usage == nil`）

重构后 `callLLMWithPipeline` 返回 `nil`，调用方判断 `nil` 时不调 `appendNewResponseMessage`。行为完全一致。

### 5. 🟢 空回复（`len(reply) == 0`）— 无差异

当前：行 228 `if len(reply) > 0` 不进入，`appendNewResponseMessage` 和 `OnWebSource` 都不执行，仅发送 `done`。
重构后：返回 `nil`，调用方不调 `appendNewResponseMessage`。行为完全一致。

### 6. 🟡 理论上唯一的细微差异

**前提**：`appendNewResponseMessage` 行 228 `panic("new response message's ID is 0")` — 这属于编程错误，非运行时条件。

- **当前**：panic 发生在 SSE events 之前 → 前端收到不完整的流（缺少 sources 和 done）
- **新方案**：panic 发生在 SSE events 之后 → 前端已收到 sources + done，但 `session.history` 未更新

这不会在实际运行中发生，因为 `userMsgID` 由行 298-299 保证 > 0：

```go
if req.Message.ID <= 0 {
    panic("new message's ID is zero still after append to history")
}
```

## 架构收益

1. **关注点分离**：[`callLLMWithPipeline`](internal/agent/chatllm.go:172) 仅负责 LLM 调用 + 流式输出（SSE），不负责任何 session/history 管理。
2. **职责清晰**：[`OnNewMessage`](internal/agent/on_chat.go:274)（调用方）负责 `appendNewRequestMessage` 和 `appendNewResponseMessage`，对称且一致。
3. **可测试性**：`callLLMWithPipeline` 不依赖 `session`，更容易单测。

## 结论

**重构安全，可执行。** 该变更无实际边界效应，架构上更清晰。`session.mu` 锁保证了所有操作的原子性和可见性。
