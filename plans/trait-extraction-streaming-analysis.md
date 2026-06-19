# 特征提取流式通信收益分析报告

## 1. 当前架构数据流

```
┌─────────────┐     SSE (EventSource)     ┌──────────────────┐     ChatStreamWithOptions     ┌───────────┐
│   Frontend   │ ◄─────────────────────── │  remote-server   │ ◄────────────────────────── │ DeepSeek  │
│  (demo page) │     /api/traits?db=&sn=   │  handleTraitsSSE │     streaming chunks         │   API     │
└─────────────┘                           └──────────────────┘                              └───────────┘
```

### 1.1 前端 → Agent (SSE)

使用浏览器原生 [`EventSource`](cmd/remote-server/demo/index.html:437) 连接 `/api/traits` 端点。

### 1.2 Agent → LLM API (Streaming)

使用 [`DeepSeekClient.ChatStreamWithOptions()`](infra/llm/deepseek.go:239) 发起流式请求，关键参数：

| 参数 | 设置值 | 说明 |
|------|--------|------|
| `tool_choice` | `ForceToolChoice("trip_traits")` | 强制 LLM 调用 `trip_traits` 工具 |
| `thinking` | `disabled` | 禁用思考链 |
| `tools` | `[trip_traits]` | 只有一个工具 |

## 2. 流式处理详细分析

### 2.1 LLM 返回的 Streaming Chunk 内容

在 `ForceToolChoice` + `thinking: disabled` 条件下，DeepSeek API 返回的 chunk 通常包含：

| Chunk 字段 | 通常内容 | 是否被流式转发到前端 |
|-----------|---------|-------------------|
| `delta.reasoning_content` | **空**（thinking disabled） | 是，但无数据可发 |
| `delta.content` | **空**（force_tool_choice 禁止文本输出） | 是，但无数据可发 |
| `delta.tool_calls[]` | **工具调用参数片段**（JSON 字符串的流式片段） | **否**，仅在后端累积 |
| `finish_reason` | `"tool_calls"`（结束时） | 是，但只在末尾发一次 |
| `usage` | token 用量（最后一块） | 是，但只在末尾发一次 |

关键代码在 [`cmd/remote-server/main.go:332-373`](cmd/remote-server/main.go:332)：

```go
for stream.Next() {
    // 1. reasoning_content → 转发（但为空）
    // 2. delta.content → 转发（但为空）
    // 3. tool_calls deltas → 累积到 toolCalls 切片，不转发
    // 4. finish_reason → 转发（末尾）
    // 5. usage → 转发（末尾）
}
```

### 2.2 工具调用参数的流式合并

[`mergeToolCallDeltas()`](cmd/remote-server/main.go:419) 将流式 `tool_calls` delta 合并到内存切片中：

```
第1块: tool_calls[{index:0, name:"trip_traits", arguments:"{\"fea"}]
第2块: tool_calls[{index:0, arguments:"tures\":[{\"cate"}]
第3块: tool_calls[{index:0, arguments:"gory_id\":1,..."}]
...     → 累积到 finish_reason="tool_calls"
...
合并后: 完整的 JSON 参数字符串
```

### 2.3 工具执行与结果返回

流结束后，[`cmd/remote-server/main.go:383-413`](cmd/remote-server/main.go:383) 执行：

```go
if finishReason == "tool_calls" && len(toolCalls) > 0 {
    // 1. pipeline.Pending() → 设置参数
    // 2. pipeline.Call() → 执行 TripTraitsTool.Execute()
    // 3. 发送 tool_result 事件 ← 前端第一次看到特征结果
    // 4. 发送 done 事件
}
```

### 2.4 前端接收的 SSE 事件时序

```
时间轴 →
├── connected           ← 连接建立，立刻收到
├── (reasoning)         ← 可能收到，但为空
├── (text)              ← 可能收到，但为空
├── finish_reason       ← LLM 决定调用工具时
├── usage               ← LLM 返回 token 用量
├── tool_result         ← 工具执行完毕后 ← 前端此时才拿到数据
└── done                ← 整个过程结束
```

## 3. 核心问题：流式通信的收益分析

### 3.1 两段流式通信的实际效果

| 通信段 | 流式方式 | 实际收益 | 原因 |
|--------|---------|---------|------|
| **Agent → LLM** | `ChatStreamWithOptions` | **无收益** | ① `thinking:disabled` → 无 reasoning 内容可流<br>② `force_tool_choice` → 无 text 内容可流<br>③ tool_calls 参数是 JSON 文本，只有完整后才能解析执行 |
| **Agent → Frontend** | SSE (EventSource) | **无收益** | 没有任何数据在工具执行完成前被流式推送给前端 |

### 3.2 为什么流式没有带来好处

归根结底是因为 **这个场景不符合流式通信的适用条件**：

流式通信的价值在于 **渐进式消费**（Progressive Consumption）：
- **Text Content**：可以逐 token 渲染到 UI，用户无需等待完整回复
- **Reasoning Content**：可以展示 AI 的思考过程，提升透明度
- **Tool Call Deltas**：只有在多工具并行或需要提前展示工具调用意图时才有价值

而特征提取场景的特性：
1. **只有一个工具可调用** → `trip_traits`，不需要多工具并行
2. **工具参数是结构化 JSON** → 必须完整接收后才能解析执行
3. **工具执行结果也是结构化数据** → 无法渐进式消费，必须完整展示
4. **Thinking 被禁用** → 无 reasoning 可展示
5. **工具强制调用** → 无 text 回复

### 3.3 与原始设计的对比

原始设计文档 [`doc/plans/trait-agent-设计计划.md`](doc/plans/trait-agent-设计计划.md) 的分析：

| 维度 | 原始设计结论 | 实际实现 | 对比 |
|------|------------|---------|------|
| 是否使用流式 | **不需要流式回复** | 使用了流式 | ❌ 偏离设计 |
| 通信模式 | 非流式 `ChatWithOptions` | `ChatStreamWithOptions` | ❌ 偏离设计 |
| 受众 | 不直接展现给最终用户 | demo 页面直接展示 | ❌ 偏离设计 |
| 资源效率 | 非流式更轻量 | 流式占用持续连接 | ❌ 偏离设计 |

## 4. 结论

**你的判断是正确的。** 当前特征提取功能的两段流式通信（前端↔Agent、Agent↔LLM）**实际上没有带来任何流式收益**。

前端仍然需要等待 LLM 完整输出工具调用参数 → Agent 执行工具 → Agent 返回结果，整个过程完成后才能展示内容。

### 4.1 非流式方案的优越性

改用非流式（`ChatWithOptions`）的收益：

| 方面 | 流式 | 非流式 |
|------|------|--------|
| 连接数 | 1 SSE 连接 + 1 HTTP 流式连接 | 1 普通 HTTP 请求 |
| 超时处理 | 需处理连接中断、重连 | 标准请求/响应 |
| 错误处理 | 需处理部分流失败 | 原子性请求 |
| 代码复杂度 | 需处理 chunk 解析、delta 合并 | 一行 `client.Chat()` |
| 资源占用 | 长时间占用连接 | 完成后即释放 |

### 4.2 改进建议（如果保留 SSE 端点）

如果希望保留 SSE 端点（便于前端实时查看进度），可以在非流式调用基础上，**在工具执行前后发送更有意义的状态事件**：

```
时间轴 →
├── connected           ← 连接建立
├── status: reading_db  ← 正在读取数据库
├── status: calling_llm ← 正在调用 LLM（此时 LLM 实际是非流式的）
├── status: parsing     ← 正在解析结果
├── tool_result         ← 特征结果
└── done
```

这样至少让前端知道当前进度，而不需要真正的流式 LLM 通信。
