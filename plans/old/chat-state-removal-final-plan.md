# chat-state.js 移除 — 最终清理计划

## 背景

`chat-state.js` 已删除，所有依赖已迁移。剩余两个需要清理的遗留问题。

---

## 1. `wasAborted`：从全局变量 → ChatSession 实例字段

### 问题

当前 `wasAborted` 是 [`chat-state-helper.js:104`](../frontend/static/chat-state-helper.js:104) 的模块级全局变量。如果 chatA 的流被中断（`wasAborted=true`），chatB 的 `cleanupAfterStream` 会错误地读取到 `true`，跳过标题自动修改等操作。

### 使用链路

```
sendMessage()
  → fetchStream()  // 发起 SSE 请求
  → catch → handleStreamError()
    → AbortError → setWasAborted(true)  ← 设置
  → finally → cleanupAfterStream(session, !!_wasAborted)
    → autoUpdateTitle(wasAborted)        ← 读取
    → getCurrentChatIfNeeded(wasAborted) ← 读取
```

### 迁移方案

在 [`ChatSession`](../frontend/static/chat-session.js:20) 类中添加 `wasAborted` 字段：

```javascript
constructor(sn) {
    // ... 现有字段 ...
    this.wasAborted = false;  // 新增
}
```

修改 [`chat-sse.js`](../frontend/static/chat-sse.js)：

| 位置 | 修改前 | 修改后 |
|------|--------|--------|
| import | `import { setWasAborted, wasAborted as _wasAborted, ... }` | 移除 `setWasAborted`, `wasAborted as _wasAborted` |
| `handleStreamError()` L336 | `setWasAborted(true)` | `session.wasAborted = true` |
| `cleanupAfterStream()` L381 | `setWasAborted(false)` | 移除（不再需要全局重置） |
| `sendMessage()` L506 | `!!_wasAborted` | `session.wasAborted` |

### 影响范围

仅 [`chat-sse.js`](../frontend/static/chat-sse.js) 一个文件需要修改。

---

## 2. `sessionDeepThinkingEnabled`：删除死代码

### 问题

- **定义**：[`chat-state-helper.js:119`](../frontend/static/chat-state-helper.js:119)
- **设置**：[`chat-sse.js:268`](../frontend/static/chat-sse.js:268) — `setSessionDeepThinkingEnabled(settings ? settings.deepThink : false)`
- **读取**：**无** — 没有任何代码读取此变量

注释说"锁定本轮会话的深度思考状态（防止流式过程中用户乱点按钮导致状态漂移）"，但实际 API 请求（L277）直接读取 `settings.deepThink` 的实时值，快照从未被使用。

### 迁移方案

| 位置 | 操作 |
|------|------|
| [`chat-state-helper.js`](../frontend/static/chat-state-helper.js) L114-127 | 删除 `sessionDeepThinkingEnabled` 变量和 `setSessionDeepThinkingEnabled` 函数 |
| [`chat-sse.js`](../frontend/static/chat-sse.js) L19 | 从 import 中移除 `setSessionDeepThinkingEnabled` |
| [`chat-sse.js`](../frontend/static/chat-sse.js) L268 | 删除 `setSessionDeepThinkingEnabled(settings ? settings.deepThink : false)` 行 |

### 影响范围

仅 [`chat-state-helper.js`](../frontend/static/chat-state-helper.js) 和 [`chat-sse.js`](../frontend/static/chat-sse.js) 两个文件。

---

## 3. 输入面板状态设计确认（无需修改）

你之前问：deepThink、webSearch、sendMode 应该是全局还是 per-chat？

**结论：保持全局（用户偏好），当前设计正确。**

| 状态 | 存放位置 | 理由 |
|------|---------|------|
| `deepThink` | `Alpine.store('settings').deepThink` | 用户偏好，全局 |
| `webSearch` | `Alpine.store('settings').webSearch` | 用户偏好，全局 |
| `sendMode` | `Alpine.store('settings').sendMode` → `sendModeAlternate` | 用户偏好，全局 |

如果 per-chat，用户切换 chat 后需要重新设置，体验差且无实际收益。

---

## 执行顺序

1. 删除 `sessionDeepThinkingEnabled`（简单删除，无副作用）
2. 迁移 `wasAborted` 到 `ChatSession` 实例字段
3. 运行构建验证
