# BrainOnline Proxy HTTP Server — 架构设计方案

## 1. 整体架构概览

```
┌─────────────────────────────────────────────────────────────────────┐
│                        前端 (React/Vue)                             │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                    Chat 对话页面                               │   │
│  │  ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐                    │   │
│  │  │用户  │  │LLM   │  │用户  │  │LLM   │  ... 无限向下       │   │
│  │  │消息1 │  │回复1 │  │消息2 │  │回复2 │                     │   │
│  │  └──────┘  └──────┘  └──────┘  └──────┘                    │   │
│  │                                                              │   │
│  │  输入框 [________________________] [发送]                    │   │
│  └──────────────────────────────────────────────────────────────┘   │
│         │ POST /api/chat (JSON)                                     │
│         │ ← SSE 流式响应 (text/event-stream)                        │
└─────────┼───────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    BrainOnline Proxy Server (Go)                        │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  /api/chat  Handler                                           │   │
│  │                                                               │   │
│  │  ① 解析前端请求 → 提取 messages + 参数                        │   │
│  │  ② 提取用户最后一条消息作为 query                              │   │
│  │  ③ 调用 VectorStore.SearchByText() 做 RAG 检索                │   │
│  │  ④ 将检索结果拼入 system prompt（上下文增强）                  │   │
│  │  ⑤ 调用 DeepSeek ChatStream() 获取流式响应                    │   │
│  │  ⑥ 逐块读取 StreamChunk，加工处理后                           │   │
│  │     以 SSE 格式流式写回前端                                    │   │
│  │  ⑦ 流结束后，将完整对话（含引用来源）存入历史                  │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  VectorStore (已有代码，复用)                                  │   │
│  │  - sqlite-vec HNSW 索引                                       │   │
│  │  - 语义搜索                                                   │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  DeepSeekClient (已有代码，复用 ChatStream)                    │   │
│  └──────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

## 2. 数据流设计（核心）

### 2.1 前端 → Proxy 请求格式

```json
POST /api/chat
Content-Type: application/json

{
  "messages": [
    {"role": "system", "content": "你是一个AI助手..."},
    {"role": "user", "content": "什么是向量数据库？"},
    {"role": "assistant", "content": "向量数据库是..."},
    {"role": "user", "content": "那它和传统数据库有什么区别？"}
  ],
  "stream": true
}
```

### 2.2 Proxy 内部处理流程

```
前端请求
    │
    ▼
┌─────────────────────────────────────────────┐
│ ① 提取最后一条 user message 作为搜索 query   │
│    query = "那它和传统数据库有什么区别？"      │
└─────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────┐
│ ② VectorStore.SearchByText(query, topK=5)   │
│    返回: [{Document, Score}, ...]            │
│    - 标题: "向量数据库"                       │
│    - 内容: "向量数据库通过..."                │
│    - 相似度: 0.92                            │
└─────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────┐
│ ③ 构建增强后的 system prompt                │
│                                             │
│  system: """                                │
│  你是一个AI助手。以下是相关知识库内容，        │
│  请基于这些信息回答用户问题：                 │
│                                             │
│  [知识片段1] 标题: 向量数据库                 │
│  内容: 向量数据库通过HNSW算法...              │
│                                             │
│  [知识片段2] 标题: SQLite数据库               │
│  内容: SQLite是一个轻量级的...                │
│  """                                        │
└─────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────┐
│ ④ DeepSeekClient.ChatStream(messages)       │
│    返回: <-chan StreamChunk                  │
└─────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────┐
│ ⑤ 逐块读取 StreamChunk，加工后 SSE 写回前端 │
│                                             │
│  每个 chunk 加工逻辑:                        │
│  - 透传 choices[0].delta.content            │
│  - 可附加引用来源信息（首次返回时）           │
│  - 可附加 token 用量（结束时）               │
│                                             │
│  写回格式:                                   │
│  data: {"content":"增量文本","done":false}   │
│  data: {"content":"更多文本","done":false}   │
│  ...                                        │
│  data: {"content":"","done":true,           │
│         "sources":[{"title":"...",...}],     │
│         "usage":{"prompt_tokens":...}}       │
└─────────────────────────────────────────────┘
```

### 2.3 Proxy → 前端 SSE 响应格式

Proxy 将 DeepSeek 的原始 SSE 格式**重新封装**为前端友好的格式：

```
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive

data: {"type":"text","content":"向量"}

data: {"type":"text","content":"数据库"}

data: {"type":"text","content":"是一种专门..."}

data: {"type":"sources","sources":[{"title":"向量数据库","score":0.92,"content":"..."}]}

data: {"type":"done","usage":{"prompt_tokens":50,"completion_tokens":120}}
```

**为什么重新封装而不是透传原始 SSE？**

| 原始 DeepSeek SSE | 重新封装后的 SSE |
|---|---|
| `data: {"id":"...","object":"chat.completion.chunk","model":"...","choices":[{"index":0,"delta":{"content":"向量"},"finish_reason":null}]}` | `data: {"type":"text","content":"向量"}` |
| 字段冗余，前端解析复杂 | 字段精简，前端直接使用 |
| 包含内部细节（id, object, model, index） | 只暴露前端需要的信息 |
| 无法携带额外信息（引用来源、用量） | 可附加 sources、usage 等元数据 |

## 3. 关键设计决策

### 3.1 为什么选择"重新封装"而非"透传"

1. **解耦**：前端不依赖 DeepSeek 的特定响应格式，未来切换 LLM 提供商时前端无需改动
2. **可扩展**：可以在流中插入自定义事件类型（如 `sources`、`error`、`status`）
3. **安全**：不暴露内部 API 的 ID、model 名称等实现细节
4. **简化前端**：前端只需处理 `{"type":"text","content":"..."}` 一种格式即可

### 3.2 SSE 事件类型定义

| type | 说明 | 字段 |
|---|---|---|
| `text` | 增量文本内容 | `content: string` |
| `sources` | 引用来源（首次返回） | `sources: Array<{title, score, content?}>` |
| `done` | 流结束 | `usage?: {prompt_tokens, completion_tokens}` |
| `error` | 错误信息 | `message: string` |

### 3.3 RAG 上下文注入策略

- **不修改用户消息**：保持用户原始消息不变
- **增强 system prompt**：将检索到的知识片段注入 system 消息
- **引用标注**：在返回的 sources 事件中附带引用信息，前端可展示"AI 参考了以下资料"

### 3.4 对话历史管理

- **前端维护**：前端维护完整的 messages 数组，每次请求时发送给 Proxy
- **Proxy 无状态**：Proxy 不存储对话历史，每次请求都是独立的
- **可选持久化**：未来可在 Proxy 端增加会话管理（session_id），将历史存入数据库

## 4. 文件结构设计

```
BrainOnline/
├── main.go                    # 入口：启动 HTTP Server
├── proxy/
│   ├── server.go              # HTTP Server 配置与路由
│   ├── handler_chat.go        # POST /api/chat 处理器（核心）
│   ├── sse_writer.go          # SSE 写入工具（封装流式写入）
│   └── types.go               # 请求/响应类型定义
├── llmclient/
│   └── deepseek.go            # 已有：DeepSeek 客户端
├── embedder/                  # 已有：Embedding 客户端
├── toolsets/
│   └── netware.go             # 已有：HTTP 客户端工具
├── frontend/                  # React/Vue 前端项目（独立目录）
│   ├── package.json
│   ├── src/
│   │   ├── App.tsx
│   │   ├── components/
│   │   │   ├── ChatWindow.tsx
│   │   │   ├── MessageBubble.tsx
│   │   │   └── InputBox.tsx
│   │   └── hooks/
│   │       └── useChatSSE.ts  # SSE 消费 Hook
│   └── ...
└── go.mod
```

## 5. 核心代码设计

### 5.1 Proxy 请求/响应类型 ([`proxy/types.go`](proxy/types.go))

```go
// ChatRequest 前端发来的聊天请求
type ChatRequest struct {
    Messages []llmclient.Message `json:"messages"`
    Stream   bool                `json:"stream"` // 固定 true
}

// SSEEvent 发送给前端的 SSE 事件
type SSEEvent struct {
    Type    string   `json:"type"`              // text | sources | done | error
    Content string   `json:"content,omitempty"`
    Sources []Source `json:"sources,omitempty"`
    Usage   *Usage   `json:"usage,omitempty"`
    Message string   `json:"message,omitempty"` // error 时使用
}

// Source RAG 引用来源
type Source struct {
    Title   string  `json:"title"`
    Content string  `json:"content,omitempty"`
    Score   float64 `json:"score"`
}

// Usage Token 用量
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

### 5.2 SSE 写入器 ([`proxy/sse_writer.go`](proxy/sse_writer.go))

```go
// SSEWriter 封装 SSE 流式写入
type SSEWriter struct {
    w   http.ResponseWriter
    flusher http.Flusher
}

func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
    // 设置 SSE 必需的响应头
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    
    flusher, _ := w.(http.Flusher)
    return &SSEWriter{w: w, flusher: flusher}
}

func (s *SSEWriter) WriteEvent(event SSEEvent) error {
    data, _ := json.Marshal(event)
    _, err := fmt.Fprintf(s.w, "data: %s\n\n", data)
    if s.flusher != nil {
        s.flusher.Flush()
    }
    return err
}
```

### 5.3 核心 Chat Handler ([`proxy/handler_chat.go`](proxy/handler_chat.go))

```go
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 1. 解析请求
    var req ChatRequest
    json.NewDecoder(r.Body).Decode(&req)
    
    // 2. 创建 SSE 写入器
    sse := NewSSEWriter(w)
    
    // 3. RAG 检索：提取最后一条 user 消息
    lastUserMsg := extractLastUserMessage(req.Messages) // 已删除，此处仅为文档示例
    results, _ := h.store.SearchByText(r.Context(), lastUserMsg, 5)
    
    // 4. 构建增强后的 messages（注入知识到 system prompt）
    enhancedMsgs := injectRAGContext(req.Messages, results)
    
    // 5. 先发送 sources 事件
    if len(results) > 0 {
        sources := toSources(results)
        sse.WriteEvent(SSEEvent{Type: "sources", Sources: sources})
    }
    
    // 6. 调用 DeepSeek 流式 API
    chunkChan, err := h.dsClient.ChatStream(r.Context(), enhancedMsgs)
    
    // 7. 逐块转发
    for chunk := range chunkChan {
        for _, choice := range chunk.Choices {
            if choice.Delta.Content != "" {
                sse.WriteEvent(SSEEvent{
                    Type:    "text",
                    Content: choice.Delta.Content,
                })
            }
        }
    }
    
    // 8. 发送 done 事件
    sse.WriteEvent(SSEEvent{Type: "done"})
}
```

### 5.4 前端 SSE 消费 Hook ([`frontend/src/hooks/useChatSSE.ts`](frontend/src/hooks/useChatSSE.ts))

```typescript
// 核心逻辑：使用 EventSource 或 fetch + ReadableStream 消费 SSE
function useChatSSE() {
    const [messages, setMessages] = useState<Message[]>([]);
    const [sources, setSources] = useState<Source[]>([]);
    const [isStreaming, setIsStreaming] = useState(false);

    const sendMessage = async (userContent: string) => {
        // 添加用户消息
        setMessages(prev => [...prev, {role: 'user', content: userContent}]);
        
        // 添加一个空的 assistant 消息占位
        const assistantMsg = {role: 'assistant', content: ''};
        setMessages(prev => [...prev, assistantMsg]);
        
        setIsStreaming(true);
        
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({messages: [...messages, assistantMsg]})
        });
        
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        
        // 逐行读取 SSE
        while (true) {
            const {done, value} = await reader.read();
            if (done) break;
            
            const lines = decoder.decode(value).split('\n');
            for (const line of lines) {
                if (!line.startsWith('data: ')) continue;
                
                const event = JSON.parse(line.slice(6));
                switch (event.type) {
                    case 'text':
                        // 追加到当前 assistant 消息
                        updateLastMessage(prev => prev + event.content);
                        break;
                    case 'sources':
                        setSources(event.sources);
                        break;
                    case 'done':
                        setIsStreaming(false);
                        break;
                }
            }
        }
    };
    
    return { messages, sources, isStreaming, sendMessage };
}
```

## 6. 实施步骤

### Step 1: 创建 Proxy 包
- 新建 [`proxy/`](proxy/) 目录
- 实现 [`proxy/types.go`](proxy/types.go) — 请求/响应类型
- 实现 [`proxy/sse_writer.go`](proxy/sse_writer.go) — SSE 写入工具
- 实现 [`proxy/handler_chat.go`](proxy/handler_chat.go) — 核心 Chat Handler

### Step 2: 改造 main.go
- 初始化 VectorStore（复用现有代码）
- 初始化 DeepSeekClient
- 注册 `/api/chat` 路由
- 启动 HTTP Server（如 `:8080`）
- 添加 CORS 中间件（支持前后端分离）

### Step 3: 创建前端项目
- 在 [`frontend/`](frontend/) 目录初始化 React/Vue 项目
- 实现 Chat 页面组件
- 实现 SSE 消费 Hook
- 配置代理（开发时 proxy 到 Go 后端）

### Step 4: 测试与联调
- 启动 Go Proxy Server
- 启动前端开发服务器
- 端到端测试流式对话

## 7. 关键问题解答

### Q1: DeepSeek 流式返回的每个 chunk 是什么格式？

每个 `data:` 行后面是一个完整的 JSON 对象，结构如下：

```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion.chunk",
  "model": "deepseek-chat",
  "choices": [
    {
      "index": 0,
      "delta": {
        "content": "增量文本"
      },
      "finish_reason": null
    }
  ]
}
```

- 第一个 chunk 的 `delta` 可能包含 `role: "assistant"`（无 content）
- 中间 chunk 的 `delta` 包含 `content: "增量文本"`
- 最后一个 chunk 的 `finish_reason` 为 `"stop"`，`delta` 可能为空
- 流结束标记为 `data: [DONE]`

### Q2: Proxy 如何处理多个 choices？

DeepSeek 目前 `n=1` 时只有一个 choice。如果未来支持多个，Proxy 可以：
- 按 `choices[i].index` 分别聚合
- 在 SSE 中增加 `index` 字段区分不同流

### Q3: 错误处理策略？

- DeepSeek API 错误 → 发送 `{"type":"error","message":"..."}` 事件
- RAG 检索失败 → 降级为纯 LLM 对话（不注入上下文）
- 前端连接断开 → 通过 `r.Context().Done()` 检测并取消流
