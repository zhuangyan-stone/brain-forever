# 消息时间戳 Timezone 审计报告

## 概述

所有存入 DB 的消息时间均为 UTC（通过 [`time.Now().UTC()`](internal/local/agent/chatllm.go:217) 写入）。本报告追踪两条路径，检查它们是否正确处理了时区。

---

## 路径 1：多轮对话历史消息

### 数据流

```
DB (chat_messages.create_at = UTC)
  → [store.Message.CreateAt]
  → [loadMessagesAsLLMMessages()]  ← 关键点
  → [llm.Message]
  → 发送给 LLM API
```

### 发现：时间被"天然忽略"

[`loadMessagesAsLLMMessages`](internal/local/agent/types.go:496) 的代码如下：

```go
for _, m := range dbMessages {
    role := llm.RoleUser
    if m.Role == 1 {
        role = llm.RoleAssistant
    }
    result = append(result, llm.Message{Role: role, Content: m.Content})
}
```

[`llm.Message`](infra/llm/client.go:121) 结构体只有：
```go
type Message struct {
    Role             string     `json:"role"`
    Content          string     `json:"content"`
    ReasoningContent string     `json:"reasoning_content,omitempty"`
    ToolCallID       string     `json:"tool_call_id,omitempty"`
    ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}
```

**结论：是的，时间被天然忽略了。** 从 DB 读出的 `m.CreateAt` 完全没有传递给 `llm.Message`。LLM API 收到的消息只有 `role` 和 `content`，没有时间戳。

### 为什么 AI 不需要消息时间？

从当前实现来看，理由可能是：
1. **对话的时序由消息顺序隐式表达** — 数组顺序就是对话顺序，LLM 不需要显式时间戳来理解先后关系
2. **减少 Token 消耗** — 每条消息加时间戳会增加 token 数
3. **LLM 不依赖绝对时间做响应** — 对于普通对话（非时间敏感场景），时间戳对回答质量影响不大

但要注意：**如果用户问"刚才那个消息是什么时候发的？"**，AI 会因为没有时间信息而无法回答。

---

## 路径 2：个人特征提取

### 数据流

```
DB (chat_messages.create_at = UTC)
  → [store.Message.CreateAt]  (UTC time.Time)
  → [OnExtractTraits]  →  Format("2006-01-02 15:04:05")
  → [traitsMsg.CreateAt]  (string, 无时区后缀)
  → Remote Server [handleTraitsJSON]
  → 解析时间 + 拼接到 content 前缀
  → [llm.Message] 发给 LLM
```

### 关键代码分析

#### 步骤 1：本地服务器格式化时间（[`on_traits.go:246-247`](internal/local/agent/on_traits.go:246)）

```go
createAt := ""
if !m.CreateAt.IsZero() {
    createAt = m.CreateAt.Format("2006-01-02 15:04:05")
}
```

`m.CreateAt` 是 `time.Time`，原始值为 UTC。`Format()` 不转换时区，**输出的字符串是 UTC 时间，但没有 `UTC` 或 `Z` 后缀**。

例如：UTC 时间 `2026-06-24T09:43:09Z` → 字符串 `"2026-06-24 09:43:09"`

#### 步骤 2：远程服务器使用时间（[`main.go:220-227`](cmd/remote-server/main.go:220)）

```go
if m.CreateAt != "" {
    if t, err := parseCreateTime(m.CreateAt); err == nil {
        content = "[" + t.Format("2006-01-02 15:04:05") + "] " + content
    }
}
```

[`parseCreateTime`](cmd/remote-server/main.go:337) 尝试多种格式解析（包括 RFC3339），但输入字符串 `"2026-06-24 09:43:09"` 没有时区信息，Go 的 `time.Parse` 会将其解析为 `time.Date(2026, 6, 24, 9, 43, 9, 0, time.UTC)`。然后重新格式化为 `[2026-06-24 09:43:09]` 拼到 content 前面。

**结论：时间保持 UTC 没有转换**。解析时因为没有时区信息，Go 默认当作 UTC；格式化时也没有做时区转换。

#### 步骤 3：System Prompt 的当前时间（[`traits.go:20`](cmd/remote-server/traits.go:20)）

```go
"CurrentLocalTime": time.Now().In(time.Local).Format("2006-01-02 15:04:05 (MST)"),
```

这里**正确地**使用了 `time.Now().In(time.Local)` 获取本地时间。所以 LLM 被告诉的"当前时间"是本地时间，但消息的时间戳是 UTC，二者不一致。

---

## 总结

| 路径 | 时间是否被忽略/未转换 | 问题 |
|------|----------------------|------|
| 多轮对话历史 | ✅ **被忽略** | `CreatedAt` 完全不传递给 LLM |
| 特征提取 | ⚠️ **未转换** | 消息时间保持 UTC，但 System Prompt 中的当前时间是本地时间（Asia/Shanghai UTC+8），两者不一致 |

## 建议

### 路径 1 改进方案

如果希望 AI 感知消息时间，需要在 `loadMessagesAsLLMMessages` 中将时间戳拼入 content：

```go
// 示例：在 content 前添加时间前缀
if !m.CreateAt.IsZero() {
    localTime := m.CreateAt.In(time.Local)
    content = fmt.Sprintf("[%s] %s", localTime.Format("2006-01-02 15:04:05"), m.Content)
} else {
    content = m.Content
}
result = append(result, llm.Message{Role: role, Content: content})
```

但需要评估 Token 成本和对对话质量的实际影响。

### 路径 2 改进方案

在 [`on_traits.go:247`](internal/local/agent/on_traits.go:247) 处将 UTC 时间转换为本地时间：

```go
// 当前（UTC，无时区后缀）：
createAt = m.CreateAt.Format("2006-01-02 15:04:05")

// 建议（本地时间，带时区后缀）：
createAt = m.CreateAt.In(time.Local).Format("2006-01-02 15:04:05 (MST)")
```

这样消息时间和 System Prompt 中的 `CurrentLocalTime` 就一致了，LLM 可以正确理解时间上下文。
