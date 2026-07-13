# Chat Tag 分类优化：智能截取 + 基础标签 + 用户已有标签注入

## 问题背景

当前 [`OnMakeChatTags`](internal/local/agent/on_tag.go:38) 在调用 LLM 进行话题分类时，使用**标题 + 全部对话内容**作为输入，并依赖一个 11 大类 × 10 子类的复杂分类框架。这导致：

1. **对话中的例子内容干扰分类** — 例如元话题"人与AI的50种分类"中举了政治例子，LLM 误归为政治类
2. **分类框架过于僵化** — 11 大类的固定结构不够灵活
3. **标签 vs 关键词概念混淆** — 标签应有更高的抽象概括能力
4. **全部消息传递浪费 token** — 长对话中大量无关消息也被送入 LLM

## 新方案核心设计

```mermaid
flowchart TD
    A[用户点击"分类"按钮] --> B[OnMakeChatTags 处理]
    B --> C[获取 chatTitle]
    C --> D[查询 SelectTagsGroup 获取用户已有标签]
    D --> E[加载对话消息 智能截取]
    E --> F{消息条数判断}
    F -->|≤20条| G[全部消息 每条≤200 rune]
    F -->|>20条| H[取前10条+后10条 每条≤200 rune]
    G --> I[构建系统提示词 含基础标签+已有标签+三级优先级]
    H --> I
    I --> J[发送LLM: 系统提示词 + 标题 + 截取的消息]
    J --> K[LLM 调用 chat_tag 工具]
    K --> L[解析标签结果并返回]
```

### 核心变化

1. **智能消息截取** — 不再全部发送
   - ≤20 条用户消息：全部发送，每条截取前 200 rune
   - >20 条用户消息：只发送前 10 条 + 后 10 条，每条截取前 200 rune
2. **去掉 11 大类的分类框架**，改用**基础标签列表**
3. **注入用户已有标签**到系统提示词中（含使用次数），明确**三级优先级**
4. **标签选择优先级**：基础标签（默认）→ 用户已有标签 → 自定义生成
5. **最多 1-2 个标签**
6. **`TagItem` 简化**：从 `{category, tag}` 变为纯字符串 `tag`
7. **仍然新增 `get_chat_samples_messages` 工具** — LLM 在截取内容不足时可主动获取更多对话样本（用于极端情况）

### 三级优先级说明

在系统提示词中明确告知 LLM：

```
标签选择优先级：
1. 基础标签（默认）— 从以下列表中选取最合适的基础标签
2. 用户已有标签 — 如果基础标签都不太合适，从用户之前使用的标签中选择
3. 自定义生成 — 如果以上都不合适，自行生成最合适的标签
```

同时提供：
```
用户已有标签（按使用频率排序）：
- 技术（5次）
- 生活（3次）
- 娱乐（2次）
```

### 多轮工具调用流程

```
第1轮: 提供 [get_chat_samples_messages, chat_tag], tool_choice="required"
  ├─ LLM 直接调用 chat_tag → 解析返回 ✓
  └─ LLM 调用 get_chat_samples_messages
       → 从DB获取完整的未截取消息样本
       → 追加到 messages
       → 第2轮: ForceToolChoice("chat_tag") → 解析返回 ✓
```

## 详细变更清单

### 1. [`lang/local/zh-CN/system_prompt.toml`](lang/local/zh-CN/system_prompt.toml:38) — [tag] 重写

**当前**：11 大类分类框架，基于标题+对话内容，输出 `{category, tag}` 格式

**改为**：
- 明确标签(tag) vs 关键词(keyword)的区别
- 基础标签列表：生活、学习、工作、技术、感情、兴趣、娱乐、文化、科学、体育、音乐、旅游、政治、社会、道德、哲学、命运、育儿、教育、人际关系、领悟
- 三级标签选择优先级说明
- 最多 1-2 个标签
- 消息不足时可调用 `get_chat_samples_messages`
- `{{.TagsUsage}}` 模板变量：展示用户已有标签（含使用次数）
- 输出格式：JSON 字符串数组 `["tag1", "tag2"]`
- 消息将附带标题一起提供（由后端处理截取）

### 2. [`lang/local/en/system_prompt.toml`](lang/local/en/system_prompt.toml:49) — [tag] 重写

与中文版同步变更。

### 3. [`lang/local/zh-CN/tools/chat_tag.toml`](lang/local/zh-CN/tools/chat_tag.toml:1) — 更新描述

- 移除 `param_title_description` 和 `param_conversation_description`（不再使用）
- 更新 `description` 文本
- `result_category` 和 `result_tag` 合并为 `result_tag`（标签本身）

### 4. [`lang/local/en/tools/chat_tag.toml`](lang/local/en/tools/chat_tag.toml:1) — 更新描述

与中文版同步。

### 5. [`internal/local/agent/toolimp/chat_tag.go`](internal/local/agent/toolimp/chat_tag.go:21) — 简化 TagItem

```go
// 原结构
type TagItem struct {
    Category string `json:"category"`
    Tag      string `json:"tag"`
}

// 新结构 — 仅保留 tag 字符串（数组元素直接是字符串）
// tags 参数变为 []string
```

Tool definition 中的 schema 从：
```json
{
  "tags": {
    "type": "array",
    "items": {
      "type": "object",
      "properties": {
        "category": {"type": "string"},
        "tag": {"type": "string"}
      },
      "required": ["category", "tag"]
    }
  }
}
```

改为：
```json
{
  "tags": {
    "type": "array",
    "items": {"type": "string"},
    "description": "分类标签数组，每个元素是一个标签字符串"
  }
}
```

### 6. **新建** [`internal/local/agent/toolimp/chat_samples.go`](internal/local/agent/toolimp/chat_samples.go)

新工具 `get_chat_samples_messages`：

```go
const ChatSamplesToolName = "get_chat_samples_messages"

type ChatSamplesToolImp struct {
    def      llm.ToolDefinition
    lang     string
    Messages []string  // 存储获取到的消息样本
}
```

- 无参数（empty properties）
- `Execute()` 返回存储的消息样本

工具实现需要由外部设置 Messages：

```go
func (f *ChatSamplesToolImp) SetMessages(msgs []string) {
    f.Messages = msgs
}
```

### 7. **新建** [`lang/local/zh-CN/tools/chat_samples.toml`](lang/local/zh-CN/tools/chat_samples.toml)

```toml
[description]
other = "获取当前对话的消息样本（用户消息和AI回复），用于辅助判断话题所属领域。当提供的对话内容不足以判断时可以调用此工具获取更多上下文。"

[pending]
other = "正在获取对话样本……"
```

### 8. **新建** [`lang/local/en/tools/chat_samples.toml`](lang/local/en/tools/chat_samples.toml)

英文翻译。

### 9. [`infra/i18n/i18n.go`](infra/i18n/i18n.go:90) — 注册新工具

在 `NewTLTools()` 调用中添加 `"chat_samples"`。

### 10. [`internal/local/agent/on_tag.go`](internal/local/agent/on_tag.go:38) — 核心逻辑重写

**当前**：
1. 加载全部消息（`ListMessages`）
2. 拼接标题 + 所有用户消息（每条截取 100 rune）
3. ForceToolChoice("chat_tag") 单次调用

**改为**：

#### 10a. 加载消息 + 智能截取

```go
dbMessages, err := session.chatsStore.ListMessages(dbSessionID)

// 筛选用户消息（role == 0）
var userMessages []string
for _, m := range dbMessages {
    if m.Role == 0 {
        content := m.Content
        if utf8.RuneCountInString(content) > 200 {
            runes := []rune(content)
            content = string(runes[:200]) + "..."
        }
        userMessages = append(userMessages, content)
    }
}

// 智能截取
var selectedMessages []string
if len(userMessages) <= 20 {
    selectedMessages = userMessages  // 全部
} else {
    selectedMessages = append(selectedMessages, userMessages[:10]...)   // 前10条
    selectedMessages = append(selectedMessages, userMessages[len(userMessages)-10:]...)  // 后10条
}

userContent := chatTitle
if len(selectedMessages) > 0 {
    userContent += "\n" + strings.Join(selectedMessages, "\n")
}
```

#### 10b. 查询用户已有标签

```go
tagUsageMap, _ := session.chatsStore.SelectTagsGroup()
// tagUsageMap: map[string]int{"技术": 5, "生活": 3}
```

#### 10c. 构建系统提示词

```go
// 格式化标签使用情况
var tagsUsageBuilder strings.Builder
if len(tagUsageMap) > 0 {
    // 按使用次数排序
    type tagCount struct {
        Tag   string
        Count int
    }
    var sorted []tagCount
    for t, c := range tagUsageMap {
        sorted = append(sorted, tagCount{t, c})
    }
    sort.Slice(sorted, func(i, j int) bool {
        return sorted[i].Count > sorted[j].Count
    })
    
    for _, tc := range sorted {
        tagsUsageBuilder.WriteString(fmt.Sprintf("- %s %d次\n", tc.Tag, tc.Count))
    }
}

systemPrompt := i18n.SystemPrompt.TL(lang, "tag", map[string]interface{}{
    "TagsUsage": tagsUsageBuilder.String(),
})
```

#### 10d. 多轮工具调用

```go
tagTool := toolimp.MakeChatTagTool(lang)
samplesTool := toolimp.MakeChatSamplesTool(lang)

messages := []llm.Message{
    {Role: llm.RoleSystem, Content: systemPrompt},
    {Role: llm.RoleUser, Content: userContent},
}

// 第1轮：提供两个工具，required
toolDefs := []llm.ToolDefinition{
    samplesTool.GetDefinition(),
    tagTool.GetDefinition(),
}

req := llm.ChatCompletionRequest{
    Messages: messages,
    Tools:    toolDefs,
    Thinking: &llm.ThinkingConfig{Type: "disabled"},
}
req.RequiredToolChoice()

resp, err := h.charLLMClient.ChatWithOptions(r.Context(), req)
if err != nil {
    // 错误处理
}

toolCall := resp.Choices[0].Message.ToolCalls[0]

if toolCall.Function.Name == toolimp.ChatSamplesToolName {
    // 获取完整消息（不截取）作为补充
    var fullMessages []string
    for _, m := range dbMessages {
        content := m.Content
        if utf8.RuneCountInString(content) > 500 {
            runes := []rune(content)
            content = string(runes[:500]) + "..."
        }
        fullMessages = append(fullMessages, content)
    }
    fullContent := strings.Join(fullMessages, "\n")
    
    // 追加 assistant 消息 + tool 结果消息
    assistantMsg := llm.Message{
        Role: llm.RoleAssistant,
        Content: "",
        ToolCalls: []llm.ToolCall{toolCall},
    }
    toolResultMsg := llm.Message{
        Role: llm.RoleTool,
        ToolCallID: toolCall.ID,
        Content: fullContent,
    }
    messages = append(messages, assistantMsg, toolResultMsg)
    
    // 第2轮：强制 chat_tag
    req2 := llm.ChatCompletionRequest{
        Messages: messages,
        Tools:    []llm.ToolDefinition{tagTool.GetDefinition()},
        Thinking: &llm.ThinkingConfig{Type: "disabled"},
    }
    req2.ForceToolChoice(toolimp.ChatTagToolName)
    
    resp2, err := h.charLLMClient.ChatWithOptions(r.Context(), req2)
    // 解析 tool call...
}

// 解析 tags
tagTool.SetArgument(toolCall.Function.Arguments)
tags := tagTool.Tags
```

#### 10e. 所需新 import

- `"sort"` — 对标签按使用次数排序

### 11. 前端 [`frontend/static/chat-api.js`](frontend/static/chat-api.js:527)

**当前类型注释**：
```js
@returns {Promise<{sn: string, tags: Array<{category: string, tag: string}>}|null>}
```

**改为**：
```js
@returns {Promise<{sn: string, tags: Array<string>}|null>}
```

## 影响范围分析

| 影响点 | 评估 |
|--------|------|
| **API 接口** | `GET /api/chat/tags?sn=XXX` — `tags` 数组元素从 `{category, tag}` 变为纯字符串 `tag` |
| **前端** | 仅修改类型注释，当前仅 `console.log` 输出，不影响 UI 渲染 |
| **DB 负载** | 仍需要 `ListMessages` 查询，但不再 `convertDBMessagesToAgentMessages` |
| **Token 消耗** | ✅ 降低 — 长对话时只发前10+后10条，每条200 rune；短对话不变 |
| **分类准确度** | ✅ 提升 — 注入用户已有标签有助于上下文相关的分类决策 |
| **消息截取丢失上下文** | ⚠️ 长对话中间部分被截掉，但首尾通常能反映话题；必要时 LLM 可调用 `get_chat_samples_messages` |

## 实施顺序

1. 修改中文 system prompt `[tag]` 部分
2. 修改英文 system prompt `[tag]` 部分
3. 修改 `chat_tag.toml` 翻译（中英文）
4. 修改 `chat_tag.go` — 简化 TagItem 及 tool definition
5. 创建 `chat_samples.go` — 新工具实现
6. 创建 `chat_samples.toml` 翻译（中英文）
7. 修改 `i18n.go` — 注册新工具
8. 修改 `on_tag.go` — 核心逻辑重写（智能截取 + 标签注入 + 多轮工具调用）
9. 修改前端类型注释
10. 构建验证

## 风险与注意事项

1. **标题为空**：当标题为空时，使用第一条用户消息的前 50 rune 作为后备
2. **用户无已有标签**：`SelectTagsGroup()` 返回空 map 时，`TagsUsage` 模板变量为空字符串，系统提示词中对应段落变为空
3. **非流式工具调用循环**：当前 DeepSeek 的 `ChatWithOptions` 不内置工具循环，需要手动编码实现第2轮调用
4. **工具调用结果处理**：`get_chat_samples_messages` 执行后需要正确构造 assistant+tool 消息对
5. **`sort` import**：需要新增 `"sort"` 包引用以排序标签使用次数
