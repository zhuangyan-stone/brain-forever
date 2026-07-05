# 合并 remote-server 功能到 local-server

## 目标

将 remote-server 的 trait 特征提取和 portrait 画像生成功能直接合并到 local-server 中，让 local-server 通过已有的 `charLLMClient` 直接调用 DeepSeek API，不再依赖 remote-server。

## 实施步骤

### Step 1: 拷贝工具实现文件

将以下两个文件从 remote 拷贝到 local 的 toolimp 包：

| 源文件 | 目标文件 |
|--------|----------|
| [`internal/remote/agent/toolimp/trip_traits.go`](../internal/remote/agent/toolimp/trip_traits.go) | [`internal/local/agent/toolimp/trip_traits.go`](../internal/local/agent/toolimp/trip_traits.go) |
| [`internal/remote/agent/toolimp/trip_highlights.go`](../internal/remote/agent/toolimp/trip_highlights.go) | [`internal/local/agent/toolimp/trip_highlights.go`](../internal/local/agent/toolimp/trip_highlights.go) |

无需修改代码，纯拷贝。包名同为 `toolimp`，依赖一致。

### Step 2: 拷贝 i18n 翻译文件

将 remote 的系统提示词和工具翻译文件拷贝到 local 的语言目录：

| 源文件 | 目标文件 | 说明 |
|--------|----------|------|
| [`lang/remote/zh-CN/tools/trip_traits.toml`](../lang/remote/zh-CN/tools/trip_traits.toml) | [`lang/local/zh-CN/tools/trip_traits.toml`](../lang/local/zh-CN/tools/trip_traits.toml) | 工具描述翻译 |
| [`lang/remote/zh-CN/tools/trip_highlights.toml`](../lang/remote/zh-CN/tools/trip_highlights.toml) | [`lang/local/zh-CN/tools/trip_highlights.toml`](../lang/local/zh-CN/tools/trip_highlights.toml) | 工具描述翻译 |

在已有的 [`lang/local/zh-CN/system_prompt.toml`](../lang/local/zh-CN/system_prompt.toml) 中追加以下 section，内容从 [`lang/remote/zh-CN/system_prompt.toml`](../lang/remote/zh-CN/system_prompt.toml) 拷贝：
- `[trip_trait]` - trait 提取的 system prompt
- `[portrait]` - 画像生成的 system prompt
- `[portrait_user_prompt]` - 画像生成的 user prompt
- `[highlights]` - 画像元数据提取的 system prompt

### Step 3: 补充 local 翻译文件中 portrait 所需的 i18n key

[`lang/local/zh-CN.toml`](../lang/local/zh-CN.toml) 中需要补充以下 portrait 格式化所需的翻译 key（参考 [`lang/remote/zh-CN.toml`](../lang/remote/zh-CN.toml)）：

- `trait_cat_1` ~ `trait_cat_14`（类别名称）
- `trait_cat_desc_1` ~ `trait_cat_desc_14`（类别描述）
- `trait_halflife_1` ~ `trait_halflife_4`（半衰期名称）
- `trait_halflife_desc_1` ~ `trait_halflife_desc_4`（半衰期描述）
- `trait_confidence_high` / `trait_confidence_medium` / `trait_confidence_low`
- `trait_item_format`（单个 trait 的格式化模板）
- `chat_titles_header`（标题列表头部）
- `chat_title_item_format`（标题格式化模板）

### Step 4: 重写 `on_traits.go`

**文件**: [`internal/local/agent/on_traits.go`](../internal/local/agent/on_traits.go)

**变更**：将 HTTP 调用 remote-server 替换为直接 LLM 调用。

**需要移除**：
- `callTraitsRemote()` 函数
- `traitsRemoteRequest`、`traitsRemoteResponse` 结构体
- HTTP 相关导入

**需要新增/调整**：
- 在 `OnExtractTraits` 中构建 `llm.ChatCompletionRequest`
- 使用 `toolimp.TripTraitsTool` 获取工具定义
- 使用 `h.charLLMClient.ChatWithOptions()` 调用 DeepSeek
- 使用 `ForceToolChoice` 强制调用 `trip_traits` 工具
- 解析 Response 中的 ToolCalls 提取结果
- 保持后续 embedding + 存储逻辑不变

**参考实现**：仿照 [`internal/remote/agent/on_traits.go`](../internal/remote/agent/on_traits.go) 中 `OnTripTraits()` 的 LLM 调用逻辑。

### Step 5: 重写 `on_portrait.go`

**文件**: [`internal/local/agent/on_portrait.go`](../internal/local/agent/on_portrait.go)

**变更**：将 HTTP SSE 代理替换为直接 LLM 流式调用。

**需要移除**：
- `callPortraitRemote()` 函数
- `portraitRemoteRequest` 结构体
- HTTP 客户端相关代码
- `os.Getenv("REMOTE_SERVER_URL")` 引用

**需要新增/调整**：
- 使用 `h.charLLMClient.ChatStreamWithOptions()` 直接流式调用
- 构建 system prompt + user prompt（从 `i18n.SystemPrompt.TL`）
- 流式读取 `ChatCompletionChunkDecoder`，转发为 SSE 事件（格式 `{"event":"text","data":"..."}`）
- 流完成后，进行第二次 LLM 调用（非流式）提取 highlights
- 使用 `toolimp.TripHighlightsTool` 解析 highlights

**需要从 remote 迁移的辅助函数**：
- `portraitTraitItem.ToString()` - 格式化单个 trait 为自然语言
- `formatTraitItems()` - 格式化所有 traits
- `formatChatTitles()` - 格式化聊天标题
- `confidenceLevelKey()` - 置信度级别文字
- `extractPortraitHighlights()` - 提取 highlights（非流式 LLM 调用）
- `sendPortraitSSE()` - 发送 SSE 事件
- `PortraitHighlights` 结构体

**参考实现**：仿照 [`internal/remote/agent/on_portrait.go`](../internal/remote/agent/on_portrait.go) 中 `OnTripPortrait()` 的流式 LLM 调用逻辑。

### Step 6: 构建验证

- 确保 `go build ./cmd/local-server/...` 编译通过
- 确保 `go build ./cmd/remote-server/...` 仍然编译通过（remote-server 保留）
- 检查所有导入路径正确

## 不变的部分

- API 路由：`/api/chat/traits` 和 `/api/user/portrait` 保持不变
- 请求/响应格式：前端完全无感知
- remote-server 代码：保留不变，可独立运行

## 数据流对比

**Trait 提取**：
- 变更前：`local-server → HTTP → remote-server → DeepSeek API`
- 变更后：`local-server → DeepSeek API`（减少一次网络跳转）

**Portrait 生成**：
- 变更前：`local-server → HTTP(SSE) → remote-server → DeepSeek API(SSE) → remote-server → HTTP(SSE) → local-server`
- 变更后：`local-server → DeepSeek API(SSE) → local-server`（减少两次网络跳转）
