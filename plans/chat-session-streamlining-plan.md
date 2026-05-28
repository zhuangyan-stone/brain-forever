# ChatSession 精简 + reasoningState 计划

## 目标

1. **消除 ChatSession 与 ChatData 之间的数据重复**：将 `isStreaming`、`streamingMsg`、`_isActive` 从 `ChatSession` 中删除，全部归 `Alpine.store('chats')` 下的 `ChatData` 管理
2. **修复 reasoning 标题持久化问题**：在 `ChatData.streamingMsg` 中添加 `reasoningState` 枚举，确保中断标题在切换 chat 后不丢失

---

## 第一部分：ChatSession 精简

### 1.1 精简后的 ChatSession

[`chat-session.js`](frontend/static/chat-session.js) 只保留连接级和 DOM 级瞬态资源：

```javascript
export class ChatSession {
    constructor(sn) {
        this.sn = sn;
        this.abortController = null;   // SSE 连接控制
        this.renderTimer = null;       // 渲染节流定时器
        this.assistantBubble = null;   // DOM 引用
        this.contentDiv = null;        // DOM 引用
        this.wasAborted = false;       // 本次连接的中断标记
    }
    // responser 由 ChatSessionManager.getOrCreate() 设置
}
```

**删除的字段**：
- `isStreaming` → 从 `ChatData.isStreaming` 读取
- `streamingMsg` → 从 `ChatData.streamingMsg` 读取
- `_isActive` → 通过比较 `chats.active.sn === session.sn` 判断
- `resetStreaming()` → 由 `chats.startStreaming(sn)` 替代
- `clearRenderTimer()` → 保留（连接级资源清理）

### 1.2 修改文件清单

| # | 文件 | 改动 |
|---|------|------|
| 1 | [`chat-session.js`](frontend/static/chat-session.js) | 删除 `isStreaming`、`streamingMsg`、`_isActive`、`resetStreaming()`；保留 `clearRenderTimer()`、`releaseDOM()` |
| 2 | [`chat-session-manager.js`](frontend/static/chat-session-manager.js) | `switchTo()` 中删除 `_isActive` 设置；`cleanup()` 中 `session.streamingMsg.isDone` → 读 Alpine store；`isStreaming` getter → 读 Alpine store |
| 3 | [`chat-sse.js`](frontend/static/chat-sse.js) | `prepareChat()` 中删除 `session.isStreaming = true`、`session._isActive = true`、`session.resetStreaming()` |
| 4 | [`chat-sse-responser.js`](frontend/static/chat-sse-responser.js) | `isActive` getter 改为比较 sn；`_syncStreamingToAlpine()` 可简化（不再需要从 session 同步到 Alpine） |
| 5 | [`chat-list.js`](frontend/static/chat-list.js) | 无改动（已通过 `sessionManager.switchTo(sn)` 调用） |

### 1.3 详细改动

#### 1.3.1 chat-session.js

```diff
- this.isStreaming = false;
- this.renderTimer = null;
+ this.renderTimer = null;  // 保留

- this.streamingMsg = { ... };
- this._isActive = false;
  this.wasAborted = false;

- resetStreaming() { ... }  // 删除
  clearRenderTimer() { ... }  // 保留
  releaseDOM() { ... }  // 保留
```

#### 1.3.2 chat-session-manager.js

**`switchTo()`** — 删除 `_isActive` 操作，`streamingMsg.isDone` 改为读 Alpine store：

```diff
 switchTo(newSN) {
-    const prevSN = this.activeSessionSN;
-    if (prevSN && this.sessions.has(prevSN)) {
-        const prevSession = this.sessions.get(prevSN);
-        prevSession._isActive = false;
-    }
     this.activeSessionSN = newSN;
     const newSession = this.getOrCreate(newSN);
-    newSession._isActive = true;

     // 如果新 session 有已完成的 streamingMsg，刷新到 DOM
-    if (newSession.streamingMsg.isDone) {
+    var chatData = window.Alpine.store('chats').getOrCreate(newSN);
+    if (chatData && chatData.streamingMsg && chatData.streamingMsg.isDone) {
         newSession.responser.flushToDOM();
     }
     return newSession;
 }
```

**`cleanup()`** — `session.streamingMsg.isDone` 改为读 Alpine store：

```diff
- if (sn !== this.activeSessionSN && session.streamingMsg.isDone) {
+ var chatData = window.Alpine.store('chats').getOrCreate(sn);
+ if (sn !== this.activeSessionSN && chatData && chatData.streamingMsg && chatData.streamingMsg.isDone) {
```

**`isStreaming` getter** — 改为读 Alpine store：

```diff
 get isStreaming() {
-    const active = this.getActive();
-    return active ? active.isStreaming : false;
+    try {
+        var chats = window.Alpine.store('chats');
+        return chats && chats.active ? chats.active.isStreaming : false;
+    } catch(e) {
+        return false;
+    }
 }
```

#### 1.3.3 chat-sse.js — `prepareChat()`

```diff
- session.isStreaming = true;
- session._isActive = true;
- session.resetStreaming();
+ // isStreaming 和 streamingMsg 由 Alpine store 的 chats.startStreaming(sn) 管理
```

注意：`chats.startStreaming(sn)` 已经在下面的 Alpine store 操作块中调用了（L125），所以不需要额外改动。

#### 1.3.4 chat-sse-responser.js

**`isActive` getter**：

```diff
 get isActive() {
-    return this.session._isActive;
+    try {
+        var chats = window.Alpine.store('chats');
+        return chats && chats.active && chats.active.sn === this.session.sn;
+    } catch(e) {
+        return false;
+    }
 }
```

**`_syncStreamingToAlpine()`** — 简化：不再从 `session.streamingMsg` 同步，因为数据直接在 Alpine store 中。但 SSEResponser 的 `onReasoning`、`onText`、`onError` 等方法当前读写 `this.session.streamingMsg`，需要改为读写 `ChatData.streamingMsg`。

这是最大的改动点。当前模式：

```
SSE event → session.streamingMsg.xxx += data
          → _syncStreamingToAlpine()  // 复制到 Alpine store
```

改为：

```
SSE event → chatData.streamingMsg.xxx += data  // 直接写 Alpine store
          → (不再需要 _syncStreamingToAlpine)
```

**具体改动**：

```javascript
// 新增辅助方法：获取当前 session 对应的 chatData.streamingMsg
_getStreamingMsg() {
    var chatData = this._getChatData();
    return chatData ? chatData.streamingMsg : null;
}

// onReasoning:
- this.session.streamingMsg.reasoning += event.content || '';
- this._syncStreamingToAlpine();
+ var sm = this._getStreamingMsg();
+ if (sm) sm.reasoning += event.content || '';

// onText:
- this.session.streamingMsg.content += event.content || '';
- this._syncStreamingToAlpine();
+ var sm = this._getStreamingMsg();
+ if (sm) sm.content += event.content || '';

// onDone:
- const msg = this.session.streamingMsg;
+ const msg = this._getStreamingMsg();
+ if (!msg) return;
  msg.isDone = true;
  // ... 其余不变，但删除 this._syncStreamingToAlpine()

// onError:
- this.session.streamingMsg.error = event.message || '未知错误';
- this._syncStreamingToAlpine();
+ var sm = this._getStreamingMsg();
+ if (sm) sm.error = event.message || '未知错误';

// flushToDOM:
- const msg = this.session.streamingMsg;
+ const msg = this._getStreamingMsg();
+ if (!msg) return;
  // ... 删除 this._syncStreamingToAlpine() 调用
```

**删除 `_syncStreamingToAlpine()` 方法**（不再需要）。

---

## 第二部分：reasoningState 枚举

### 2.1 在 ChatData.streamingMsg 中添加字段

[`alpine-store.js`](frontend/static/alpine-store.js) 的 `startStreaming()` 中：

```diff
 chat.streamingMsg = {
     reasoning: '',
     content: '',
     sources: [],
     usage: null,
     msgId: 0,
     createdAt: null,
     isDone: false,
     error: null,
+    reasoningState: 'thinking',  // 'thinking' | 'done' | 'interrupted'
 };
```

### 2.2 修改 reasoning 标题设置逻辑

| 位置 | 当前 | 改为 |
|------|------|------|
| [`chat-reasoning.js:58`](frontend/static/chat-reasoning.js:58) `createReasoningArea()` | 硬编码 `'正在思考……'` | 从 `chatData.streamingMsg.reasoningState` 读取 |
| [`chat-reasoning.js:144`](frontend/static/chat-reasoning.js:144) `finalizeReasoningArea()` | 硬编码 `'思考完成'` | 设置 `reasoningState = 'done'` 并读取 |
| [`chat-sse.js:305`](frontend/static/chat-sse.js:305) `handleAbortError()` | 硬编码 `'AI 思路已被掐断'` | 设置 `reasoningState = 'interrupted'` 并读取 |
| [`chat-reasoning.js:213`](frontend/static/chat-reasoning.js:213) `restoreReasoningArea()` | 硬编码 `'思考完成'` | 从 Alpine store 读取 `reasoningState` |

### 2.3 标题文本映射

```javascript
const REASONING_TITLES = {
    thinking:    '正在思考……',
    done:        '思考完成',
    interrupted: 'AI 思路已被掐断',
};
```

### 2.4 详细改动

#### 2.4.1 chat-reasoning.js

**`createReasoningArea()`** — 接受 `reasoningState` 参数：

```diff
- export function createReasoningArea(assistantBubble) {
+ export function createReasoningArea(assistantBubble, reasoningState) {
     // ...
-    const titleText = '正在思考……';
+    const titleText = REASONING_TITLES[reasoningState] || '正在思考……';
```

**`finalizeReasoningArea()`** — 设置 `reasoningState = 'done'`：

```diff
 export function finalizeReasoningArea(assistantBubble) {
     // ...
-    titleEl.textContent = `思考完成${timeText}`;
+    // 更新 Alpine store 中的 reasoningState
+    try {
+        var chats = window.Alpine.store('chats');
+        if (chats && chats.active && chats.active.streamingMsg) {
+            chats.active.streamingMsg.reasoningState = 'done';
+        }
+    } catch(e) {}
+    titleEl.textContent = `${REASONING_TITLES.done}${timeText}`;
```

**`restoreReasoningArea()`** — 从 Alpine store 读取 `reasoningState`：

```diff
- export function restoreReasoningArea(assistantBubble, reasoningText) {
+ export function restoreReasoningArea(assistantBubble, reasoningText, reasoningState) {
     // ...
-    const titleText = '思考完成';
+    const titleText = REASONING_TITLES[reasoningState] || REASONING_TITLES.done;
```

#### 2.4.2 chat-sse.js — `handleAbortError()`

```diff
 function handleAbortError(session) {
     // ...
-    titleEl.textContent = 'AI 思路已被掐断';
+    // 更新 Alpine store 中的 reasoningState
+    try {
+        var chats = window.Alpine.store('chats');
+        if (chats) {
+            var chatData = chats.getOrCreate(session.sn);
+            if (chatData && chatData.streamingMsg) {
+                chatData.streamingMsg.reasoningState = 'interrupted';
+            }
+        }
+    } catch(e) {}
+    titleEl.textContent = 'AI 思路已被掐断';
```

#### 2.4.3 调用 `restoreReasoningArea` 的地方传递 `reasoningState`

搜索所有调用 `restoreReasoningArea` 的地方：

| 文件 | 行号 | 改动 |
|------|------|------|
| [`chat-list.js:602`](frontend/static/chat-list.js:602) | `restoreReasoningArea(assistantMsg, assistantEntry.reasoning, assistantEntry.deep_think)` | 第三个参数改为 `assistantEntry.reasoningState` |
| [`chat-list.js:651`](frontend/static/chat-list.js:651) | `restoreReasoningArea(session.assistantBubble, msg.reasoning)` | 添加第三个参数 `msg.reasoningState` |
| [`chat-list.js:699`](frontend/static/chat-list.js:699) | `restoreReasoningArea(session.assistantBubble, sm.reasoning)` | 添加第三个参数 `sm.reasoningState` |
| [`chat-restore.js:104`](frontend/static/chat-restore.js:104) | `restoreReasoningArea(msgDiv, msg.reasoning, msg.deep_think)` | 第三个参数改为 `msg.reasoningState` |
| [`chat-sse-responser.js:372`](frontend/static/chat-sse-responser.js:372) | `restoreReasoningArea(this.session.assistantBubble, msg.reasoning)` | 添加第三个参数 `msg.reasoningState` |

---

## 执行顺序

1. **精简 ChatSession**（`chat-session.js` + `chat-session-manager.js`）
2. **修改 SSEResponser**（`chat-sse-responser.js` — 直接读写 Alpine store，删除 `_syncStreamingToAlpine`）
3. **修改 chat-sse.js**（删除 `prepareChat` 中的冗余设置）
4. **添加 reasoningState**（`alpine-store.js` + `chat-reasoning.js` + `chat-sse.js`）
5. **更新 restoreReasoningArea 调用**（`chat-list.js` + `chat-restore.js` + `chat-sse-responser.js`）
6. **运行构建验证**
