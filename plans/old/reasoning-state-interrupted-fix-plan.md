# 中断状态完整修复方案

## 技术可行性确认

**后端能检测到中断吗？能。**

[`callLLMWithPipeline`](internal/agent/chatllm.go:188) 的 `ctx` 来自 HTTP handler。当前端 abort 时，HTTP 请求级别的 context 被 cancel，`ctx.Done()` 通道关闭。在 [`ChatWithPipeline`](infra/llm/deepseek.go:405) 返回后检查 `ctx.Err()` 即可知道是否被中断。

## 完整方案

### 一、数据表加 `interrupted` 字段

**`store.Message`** ([`internal/store/chats.go:42`](internal/store/chats.go:42))：
```go
type Message struct {
    // ... 现有字段 ...
    Interrupted bool `db:"interrupted"` // ← 新增：是否被中断
}
```

**`InsertMessage`** ([`internal/store/messages.go:8`](internal/store/messages.go:8))：
```sql
INSERT INTO chat_messages(chat_id, group_index, role, reasoning, content, interrupted)
VALUES(?, ?, ?, ?, ?, ?)
```

**`ListMessages`** SELECT 也加上 `interrupted` 列。

**DB migration**：`ALTER TABLE chat_messages ADD COLUMN interrupted INTEGER NOT NULL DEFAULT 0;`

### 二、后端检测中断 + 追加中断标记

**`streamChatCompletion`** ([`infra/llm/deepseek.go:336`](infra/llm/deepseek.go:336)) 增加 `ctx` 参数和中断检查：

```go
func streamChatCompletion(
    ctx context.Context,  // ← ADD
    stream *ChatCompletionChunkDecoder,
    pipeline Pipeline,
    onUsage func(Usage),
) (StreamResult, error) {
    for stream.Next() {
        select {
        case <-ctx.Done():
            // 前端中断，返回已累积的部分内容
            return StreamResult{
                Reply:        replyBuilder.String(),
                Reasoning:    reasoningBuilder.String(),
                ToolCalls:    toolCalls,
                FinishReason: "interrupted",
            }, ctx.Err()
        default:
        }
        // ... 原有逻辑 ...
    }
}
```

**`ChatWithPipeline`** 透传 `ctx` 给 `streamChatCompletion`。

**`callLLMWithPipeline`** ([`internal/agent/chatllm.go:188`](internal/agent/chatllm.go:188)) 增加中断检测：

```go
reply, reasoning, err := h.charLLMClient.ChatWithPipeline(ctx, messages, &pipeline, withDeepThink)

// 检测是否被中断
isInterrupted := ctx.Err() != nil

if err != nil && !isInterrupted {
    pipeline.OnError(err)
    return nil  // 真正的错误才跳过保存
}

// ... 计算 usage ...

if len(reply) > 0 || isInterrupted {
    assistantMsg = &Message{
        ID:        userMsgID,
        Role:      llm.RoleAssistant,
        Content:   reply,
        Reasoning: reasoning,
        CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
        Usage:     usage,
    }

    if isInterrupted {
        assistantMsg.Interrupted = true
        // 追加中断标记 —— 使用 i18n 资源
        brokenSuffix := i18n.TL(lang, "assistant_broken_suffix")
        assistantMsg.Content += "\n\n---\n" + brokenSuffix
    }

    // ... 处理 web sources ...
}
```

注意：此处 `reply` 在中断时是 `streamChatCompletion` 提前返回的**部分内容**，而非完整回复，正好符合需求。

### 三、i18n 资源

**`lang/zh-CN.toml`**：
```toml
[assistant_broken_suffix]
other = "我的回复被中断了……"
```

**`lang/en.toml`**：
```toml
[assistant_broken_suffix]
other = "My reply was interrupted..."
```

### 四、`agent.Message` 加 `Interrupted` 字段

**`internal/agent/types.go`** ([`internal/agent/types.go:21`](internal/agent/types.go:21))：
```go
type Message struct {
    ID        int64  `json:"id"`
    Role      string `json:"role"`
    Content   string `json:"content"`
    Usage     *Usage `json:"usage,omitempty"`
    Reasoning string `json:"reasoning,omitempty"`
    Sources   []toolimp.WebSource `json:"sources,omitempty"`
    CreatedAt string `json:"created_at"`
    Interrupted bool `json:"interrupted"` // ← 新增
}
```

### 五、前端同步

**`alpine-store.js` `setChatMessageGroups`** ([`frontend/static/alpine-store.js:657`](frontend/static/alpine-store.js:657))：
```javascript
lastGroup.assistant.reasoningState = msg.reasoning ? (msg.interrupted ? 'interrupted' : 'done') : undefined;
//                                                          ↑ 读取 interrupted 字段
```

### 六、模板简化

**`frontend/index.html`** 角色标签改为只两态：
```html
x-text="group.assistant.reasoningHTML
    ? (group.assistant.reasoningState === 'done'
        ? '🤖 AI 思考完成 (' + formatTime(group.assistant.createdAt) + ')'
        : '🤖 AI 正在思考……')
    : '🤖 AI' + (group.assistant.createdAt ? ' (' + formatTime(group.assistant.createdAt) + ')' : '')"
```

中断时的 `interrupted` 和正常完成的 `done` 都显示 "思考完成"，因为内容末尾已由后端追加 "---\n我的回复被中断了……"。

### 涉及文件清单

| 文件 | 改动 |
|------|------|
| `infra/llm/deepseek.go` | `streamChatCompletion` 增加 `ctx` 参数 + 中断提前返回 |
| `internal/agent/chatllm.go` | `callLLMWithPipeline` 检测 `ctx.Err()`，追加中断标记 |
| `internal/agent/types.go` | `Message` 增加 `Interrupted bool` |
| `internal/store/chats.go` | `Message` 增加 `Interrupted bool` |
| `internal/store/messages.go` | `InsertMessage` + `ListMessages` 含 `interrupted` 列 |
| `internal/agent/db.go` | `persistMessageToDB` 持久化 `Interrupted` 字段 |
| `lang/zh-CN.toml` | 新增 `assistant_broken_suffix` 资源 |
| `lang/en.toml` | 新增 `assistant_broken_suffix` 资源 |
| `frontend/static/alpine-store.js` | `setChatMessageGroups` 读取 `msg.interrupted` |
| `frontend/index.html` | 角色标签简化为两态 |

### 数据流总结

```mermaid
flowchart TB
    subgraph BACKEND["后端"]
        A[LLM 流式回复]
        B[streamChatCompletion 检查 ctx.Done]
        C[Ctx 未取消] --> D[正常完成 → done 事件 → 入库]
        B -- ctx 已取消 --> E[提前返回部分内容]
        E --> F[callLLMWithPipeline 检测 isInterrupted]
        F --> G[追加 "---\n我的回复被中断了……"]
        G --> H[interrupted=true 入库]
    end

    subgraph FRONTEND["前端"]
        I[API 加载消息]
        I --> J[setChatMessageGroups]
        J --> K{msg.interrupted?}
        K -- true --> L[reasoningState = 'done']
        K -- false --> M[msg.reasoning? → 'done' : undefined]
        L --> N[标签：🤖 AI 思考完成]
        M --> N
        N --> O[内容末尾已有中断标记]
    end
```
