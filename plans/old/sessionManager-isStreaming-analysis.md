# `sessionManager.isStreaming` 作用域分析

## 背景

用户发现 [`chat-session-manager.js:138-141`](../frontend/static/chat-session-manager.js:138) 的 `isStreaming` getter 仅返回当前 **active session** 的 streaming 状态：

```javascript
get isStreaming() {
    const active = this.getActive();
    return active ? active.isStreaming : false;
}
```

问题是：非 active 的 chat 如果正在后台 streaming，是否有代码误用了这个全局 getter 去检查自身的 streaming 状态？

## 代码库中实际的 `isStreaming` 概念（三层）

| 层级 | 位置 | 语义 | 使用者 |
|------|------|------|--------|
| **Per-Session** | [`ChatSession.isStreaming`](../frontend/static/chat-session.js:25) | 该 ChatSession 自身是否正在接收 SSE | SSE 处理链直接读写 |
| **Per-Store** | [`chatData.isStreaming` (Alpine)](../frontend/static/alpine-store.js:175) | 响应式数据模型中每个 chat 的 streaming 状态 | Alpine 模板绑定（`$store.chats.active?.isStreaming`） |
| **Global (Manager)** | [`sessionManager.isStreaming`](../frontend/static/chat-session-manager.js:138) | **仅**当前 active session 的 streaming 状态 | 命令式 JS 中控制 UI 交互 |

## 消费者分类追踪

### ① `sessionManager.isStreaming` 消费者 — 全部正确

所有消费者都是**用户与当前可见 UI 交互时触发**的操作，只应阻遏与 active chat 的交互：

| 位置 | 用途 | 正确性 |
|------|------|--------|
| [`chat-sse.js:82`](../frontend/static/chat-sse.js:82) | 发送消息前防御 | 用户只能向 active chat 发消息 |
| [`chat-markdown.js:275`](../frontend/static/chat-markdown.js:275) | 复制代码按钮 | 按钮位于 active chat 的 DOM 中 |
| [`chat-ui.js:110`](../frontend/static/chat-ui.js:110) | 删除按钮 | 同上 |
| [`chat-ui.js:249,272`](../frontend/static/chat-ui.js:249) | 群组删除按钮 | 同上 |
| [`chat-ui.js:674,689`](../frontend/static/chat-ui.js:674) | 停止按钮 | 只停止 active chat |
| [`chat.js:81`](../frontend/static/chat.js:81) | AI 标题按钮 | 操作 active chat |
| [`chat.js:106`](../frontend/static/chat.js:106) | 新对话按钮 | 全局操作 |
| [`chat.js:694`](../frontend/static/chat.js:694) | 发送/停止按钮 | 操作 active chat |
| [`chat.js:747`](../frontend/static/chat.js:747) | 头部标题编辑 | 操作 active chat |
| [`chat.js:792`](../frontend/static/chat.js:792) | 登录按钮 | 全局操作 |
| [`chat.js:845,867`](../frontend/static/chat.js:845) | 滚动处理 | 仅 active chat 的滚动容器 |
| [`chat-list.js:699`](../frontend/static/chat-list.js:699) | **侧边栏重命名** | ❌ 应检查目标 chat 自身状态 |

### ② Alpine 绑定 `$store.chats.active?.isStreaming` — 全部正确

[`index.html`](../frontend/index.html:125) 中所有 Alpine 绑定（`newChatBtn`, `aiTitleBtn`, `loginBtn`, `sendBtn`, `textarea`, `stopStreamingBtn` 等）均绑定到 active chat 的 per-chat 状态。切换 active chat 时 Alpine 自动更新绑定。

### ③ 后台 SSE 处理链 — 使用 `this.session._isActive`，不依赖全局状态

[`SSEResponser`](../frontend/static/chat-sse-responser.js:39) 所有事件处理方法使用 `this.isActive`（即 `this.session._isActive`）判断是否操作 DOM：

```javascript
// onText (line 118)
if (this.isActive && this.session.contentDiv) { ... }

// onDone → _applyDoneToDOM (line 174)
if (this.isActive && this.session.assistantBubble) { ... }

// flushToDOM (line 286)
// 由 switchTo() 调用时，目标 session 已标记为 active
```

[`cleanupAfterStream()`](../frontend/static/chat-sse.js:341) 使用直接引用比较而非全局 getter：

```javascript
const isActiveSession = sessionManager.getActive() === session;
```

## 需要修复的边缘情况

### [`handleRename()` in chat-list.js:697-702](../frontend/static/chat-list.js:697)

**问题**：用户右键点击侧边栏中**后台正在 streaming** 的 chat 时，`sessionManager.isStreaming` 返回的是 active chat 的 streaming 状态（可能为 false），导致允许修改标题。应检查目标 chat 自身的 `ChatSession.isStreaming`。

**修复方案**：

```javascript
async function handleRename(chat) {
    // 检查目标对话自身的 streaming 状态（而非 active chat）
    const targetSession = sessionManager.sessions.get(chat.sn);
    if (targetSession && targetSession.isStreaming) {
        showToast('该对话正在生成回复，请稍后再修改标题', 'info');
        return;
    }
    // ... 后续不变
}
```

**注意**：同步更新注释，不再写"同 HEADER 点击标题的逻辑一致"，因为 head 标题编辑（`chat.js:747`）应继续使用 `sessionManager.isStreaming`（它操作的是 active chat）。

### 不变更的：header 标题编辑和 AI 标题按钮

[`chat.js:747`](../frontend/static/chat.js:747) 和 [`chat.js:81`](../frontend/static/chat.js:81) 操作的都是 active chat，使用 `sessionManager.isStreaming` 是正确的，**不需要修改**。

## 总结

- 整体设计**安全**：后台 SSE 处理链使用 per-session 属性，不依赖全局 getter
- 唯一待修复点：[`chat-list.js:699`](../frontend/static/chat-list.js:699) 侧边栏重命名时应检查目标 chat 自身的 streaming 状态
