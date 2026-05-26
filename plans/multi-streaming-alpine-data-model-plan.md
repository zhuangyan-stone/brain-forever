# 多会话并发流输出 + Alpine 数据模型 — 执行计划

## 1. 当前状态审计

### ✅ 已完成（无需重复工作）

| 组件 | 文件 | 状态说明 |
|------|------|---------|
| `ChatSession` 类 | [`chat-session.js`](frontend/static/chat-session.js) | SSE 连接管理、streamingMsg 累积、DOM 引用 |
| `SSEResponser` 类 | [`chat-sse-responser.js`](frontend/static/chat-sse-responser.js) | SSE 事件 → streamingMsg + DOM 更新（含 isActive 判断） |
| `ChatSessionManager` | [`chat-session-manager.js`](frontend/static/chat-session-manager.js) | switchTo() 切换活跃/非活跃、remove()、cleanup() |
| `chat-sse.js` 集成 | [`chat-sse.js:107-114`](frontend/static/chat-sse.js:107) | sendMessage 使用 sessionManager.getOrCreate() + ChatSession |
| `chat-list.js` 集成 | [`chat-list.js:447`](frontend/static/chat-list.js:447) | selectChat() 使用 sessionManager.switchTo() |
| `chat-state.js` 委托 | [`chat-state.js:108-137`](frontend/static/chat-state.js:108) | isStreaming/abortController 委托给 sessionManager |
| `chat-restore.js` 集成 | [`chat-restore.js:92-100`](frontend/static/chat-restore.js:92) | 恢复时检查未渲染的 streamingMsg |
| 后端 Phase B | [`internal/agent/types.go:91`](internal/agent/types.go:91) | chat 结构体已移除 messages，纯 DB 读写 |
| 后端并发支持 | [`internal/agent/on_chat.go:237`](internal/agent/on_chat.go:237) | 流式期间不持有 session.mu，支持并发切换 |

### ❌ 待完成

| 缺失项 | 说明 | 优先级 |
|--------|------|--------|
| `$store.chats` Alpine store | 包含 items[]、activeIndex、per-chat messages/streamingMsg | 🔴 P0 |
| 消息气泡 Alpine 化 | 用 Alpine `x-for` 模板替代 `addMessage()` 手动 DOM 创建 | 🔴 P0 |
| Per-chat isStreaming | UI 组件从 `$store.chats.active.isStreaming` 读取 | 🔴 P0 |
| SSEResponser 数据流改造 | 从直接 DOM 操作改为更新 `$store.chats` | 🟡 P1 |
| Reasoning/Sources Alpine 化 | 用 `x-show` + `x-text` 替代手动 DOM 操作 | 🟡 P1 |
| 刻度导航 Alpine 化 | 从 DOM 查询改为基于 `$store.chats.active.messages` 数据 | 🟢 P2 |

---

## 2. 架构总览

```mermaid
flowchart TB
    subgraph "SSE 层（已有，需改造）"
        SSE[SSE Stream]
        CS[ChatSession]
        SR[SSEResponser]
        SSE --> CS
        CS --> SR
    end

    subgraph "数据层（新建）"
        SC[$store.chats Alpine Store]
        CI[items: ChatData[]]
        AI[activeIndex: number]
        ACTIVE[active: ChatData - computed]
    end

    subgraph "渲染层（Alpine 模板）"
        XF[x-for messages in active.messages]
        XS[x-show reasoning / sources]
        XT[x-text streamingMsg.content]
        UI[UI 组件: sendBtn/messageInput 等]
    end

    SR -- 写入 --> CI
    AI -- 绑定 --> XF
    AI -- 绑定 --> XS
    AI -- 绑定 --> UI
    XF -- 渲染 --> DOM[消息 DOM]
    XS -- 渲染 --> DOM
    XT -- 渲染 --> DOM

    style SC fill:#c6e2ff,stroke:#4a90d9,stroke-width:2px
    style CI fill:#e8f4f8,stroke:#4a90d9
    style AI fill:#e8f4f8,stroke:#4a90d9
    style SR fill:#fff3cd,stroke:#ffc107
    style XF fill:#d4edda,stroke:#28a745
    style XS fill:#d4edda,stroke:#28a745
    style XT fill:#d4edda,stroke:#28a745
```

### 数据模型定义

```javascript
// ChatData — 每个对话的数据结构
{
  sn: string,                    // 对话 SN
  title: string,                 // 对话标题
  titleState: 0 | 1 | 2,         // 标题修改状态
  isStreaming: boolean,          // 是否正在流式接收
  messages: [                    // 已完成的消息列表
    {
      id: number,                // 组 ID（user+assistant 共享）
      role: 'user' | 'assistant',
      content: string,
      reasoning?: string,        // assistant 消息的思考链
      sources?: WebSource[],     // assistant 消息的搜索来源
      usage?: Usage,             // assistant 消息的 token 用量
      createdAt?: string,        // UTC 时间戳
    }
  ],
  streamingMsg: null | {         // 流式进行中的累积数据
    reasoning: string,
    content: string,
    sources: WebSource[],
    usage: Usage | null,
    msgId: number,
    createdAt: string | null,
    isDone: boolean,
    error: string | null,
  }
}
```

---

## 3. 分阶段执行计划

### Phase A：Alpine Chat Store 建立

**目标**：创建 `$store.chats`，作为所有对话数据的单一数据源。此阶段只建数据层，不改渲染。

**文件**：[`frontend/static/alpine-store.js`](frontend/static/alpine-store.js)

**改动**：在 `alpine:init` 事件中注册 `$store.chats`：

```javascript
Alpine.store('chats', {
  // ---- 数据 ----
  items: [],              // ChatData[] — 所有对话数据
  activeIndex: -1,        // 当前活跃索引

  // ---- 计算属性 ----
  get active() {
    return this.items[this.activeIndex] || null;
  },

  // ---- 方法 ----

  // 按 SN 获取或创建 ChatData
  getOrCreate(sn) {
    let item = this.items.find(c => c.sn === sn);
    if (!item) {
      item = {
        sn,
        title: '',
        titleState: 0,
        isStreaming: false,
        messages: [],
        streamingMsg: null,
      };
      this.items.push(item);
      // 如果 activeIndex 尚未设置，设为刚创建的项
      if (this.activeIndex < 0) this.activeIndex = 0;
    }
    return item;
  },

  // 按索引获取
  getByIndex(idx) {
    return this.items[idx] || null;
  },

  // 按 SN 切换活跃对话
  switchTo(sn) {
    const idx = this.items.findIndex(c => c.sn === sn);
    if (idx >= 0) this.activeIndex = idx;
    else console.warn('chats.switchTo: SN not found', sn);
  },

  // 初始化/覆盖整个聊天列表（来自后端 restoreChat 响应）
  initFromRestore(data) {
    // data.messages → 当前活跃 chat 的 messages
    // data.chats → chat 列表（sn, title, titleState）
    // data.current_chat_sn → 活跃 SN
  },

  // 开始流式（创建 streamingMsg）
  startStreaming(sn) {
    const chat = this.getOrCreate(sn);
    chat.isStreaming = true;
    chat.streamingMsg = {
      reasoning: '',
      content: '',
      sources: [],
      usage: null,
      msgId: 0,
      createdAt: null,
      isDone: false,
      error: null,
    };
  },

  // 结束流式（streamingMsg → messages.push, isStreaming = false）
  finalizeStreaming(sn) {
    const chat = this.getOrCreate(sn);
    if (!chat.streamingMsg) return;
    const sm = chat.streamingMsg;
    if (sm.content || sm.reasoning) {
      chat.messages.push({
        id: sm.msgId,
        role: 'assistant',
        content: sm.content,
        reasoning: sm.reasoning || undefined,
        sources: sm.sources.length > 0 ? sm.sources : undefined,
        usage: sm.usage || undefined,
        createdAt: sm.createdAt || undefined,
      });
    }
    chat.isStreaming = false;
    chat.streamingMsg = null;
  },

  // 重置所有数据（切换用户时）
  reset() {
    this.items = [];
    this.activeIndex = -1;
  },
});
```

**验证方式**：
1. 在浏览器控制台执行 `Alpine.store('chats').items` → 应返回空数组
2. `Alpine.store('chats').getOrCreate('test-sn')` → 应返回新的 ChatData 对象
3. 重复调用 `getOrCreate` 同一 SN → 应返回同一对象

---

### Phase B：消息气泡 Alpine 化

**目标**：将 `#chatContainer` 从手动 `addMessage()` 改为 Alpine `x-for` 模板。

**关键设计决策**：

1. **模板位置**：`index.html` 中 `#chatContainer` 内嵌 Alpine 模板
2. **渲染函数**：Markdown 渲染器（`renderMarkdown`）需要从 Alpine 作用域可调用 → 注册为 `window.renderMarkdown` 或 Alpine magic
3. **消息分组**：`x-for` 按 messages 数组渲染，user/assistant 同 id 的放在同一个 `.message-group` 中（可在 Alpine 模板中用 group-by 逻辑，或预处理为分组数据结构）
4. **流式气泡**：streaming 消息用单独的 `x-if` 渲染（当 `chat.streamingMsg !== null` 时显示）

**实现要点**：

#### B.1 注册 Markdown 渲染到全局

在 [`chat-markdown.js`](frontend/static/chat-markdown.js) 末尾或 `alpine-store.js` 中：

```javascript
// 让 Alpine 模板可以调用 renderMarkdown
// 注意：此函数以普通 <script> 的时机注册，确保 Alpine 扫描 DOM 前已可用
window.AlpineRenderMarkdown = function(content) {
  return renderMarkdown(content || '');
};
```

#### B.2 `index.html` 中的 Alpine 模板

```html
<!-- 聊天区域 — Alpine 数据驱动渲染 -->
<main class="chat-container" id="chatContainer"
      x-data="{}"
      x-effect="console.log('active messages changed:', $store.chats.active?.messages.length)">
  
  <!-- 流式进行中的消息组 -->
  <template x-if="$store.chats.active?.streamingMsg">
    <div class="message-group streaming-group">
      <!-- 用户消息（发送时固定的内容） -->
      <div class="message user" x-data>
        <!-- 用户消息已由 state.messages 中的最后一个 user 消息表示 -->
      </div>
      <!-- 助手的 reasoning（思考链） -->
      <div class="message assistant streaming">
        <div class="reasoning-area"
             x-show="$store.chats.active.streamingMsg.reasoning.length > 0"
             :class="{
               active: $store.chats.active.isStreaming,
               done: $store.chats.active.streamingMsg.isDone
             }">
          <div class="reasoning-title" x-text="'思考过程'"></div>
          <div class="reasoning-content"
               x-text="$store.chats.active.streamingMsg.reasoning">
          </div>
        </div>
        <div class="bubble"
             :class="{ streaming: $store.chats.active.isStreaming }"
             x-html="AlpineRenderMarkdown($store.chats.active.streamingMsg.content)">
        </div>
        <div class="message-actions">
          <!-- 复制按钮等 -->
        </div>
      </div>
    </div>
  </template>

  <!-- 已完成的消息列表 -->
  <template x-for="(msg, idx) in $store.chats.active?.messages || []" :key="msg.id + '-' + msg.role">
    <div class="message-group"
         :data-msg-id="msg.id">
      
      <!-- 用户消息 -->
      <div class="message user" x-show="msg.role === 'user'">
        <div class="message-inner">
          <div class="role-label" x-text="'我'"></div>
          <div class="bubble" x-html="AlpineRenderMarkdown(msg.content)"></div>
        </div>
      </div>

      <!-- 助手消息 -->
      <div class="message assistant" x-show="msg.role === 'assistant'">
        <div class="message-inner">
          <div class="role-label role-label-ai" x-text="'AI'"></div>
          
          <!-- Reasoning 区域 -->
          <div class="reasoning-area done"
               x-show="msg.reasoning"
               x-data="{ expanded: false }">
            <div class="reasoning-title" @click="expanded = !expanded">
              <span>思考过程</span>
              <span class="reasoning-toggle" x-text="expanded ? '收起' : '展开'"></span>
            </div>
            <div class="reasoning-content" x-show="expanded"
                 x-text="msg.reasoning">
            </div>
          </div>

          <!-- 消息气泡 -->
          <div class="bubble" x-html="AlpineRenderMarkdown(msg.content)"></div>

          <!-- Sources 区域 -->
          <div class="sources-panel" x-show="msg.sources?.length > 0">
            <div class="sources-title">参考来源</div>
            <template x-for="src in msg.sources" :key="src.url || src.title">
              <div class="source-item">
                <a :href="src.url" target="_blank" x-text="src.title"></a>
              </div>
            </template>
          </div>

          <!-- Token 用量 -->
          <div class="token-usage" x-show="msg.usage"
               x-text="'Token: ' + (msg.usage?.total_tokens || 0)">
          </div>
        </div>
      </div>
    </div>
  </template>
</main>
```

**重要**：目前 `addMessage()` 还承担着**流式气泡的 DOM 引用创建**职责（`chat-sse.js:104-105`）——`prepareChat()` 中调用 `addMessage('assistant', '', null, true)` 来创建空的 assistant 气泡，并在 `session.assistantBubble` 和 `session.contentDiv` 中保存引用，供 `SSEResponser` 后续更新。

Phase B**第一阶段**需保留此路径，即：
- `prepareChat()` 仍调用 `addMessage()` 创建空气泡
- SSEResponser 仍然操作 `session.assistantBubble` DOM
- **但** `addMessage()` 创建 DOM 的同时，也把 user 消息推入 `$store.chats.active.messages`

**后续**在 Phase C 中，当 SSEResponser 改为更新 `$store.chats` 后，`addMessage()` 创建 DOM 的路径可以完全被 Alpine 模板替代，届时移除 `addMessage()` 的流式创建逻辑。

#### B.3 迁移 addMessage 逻辑

在 [`chat-ui.js`](frontend/static/chat-ui.js) 中，`addMessage()` 增加数据写入逻辑：

```javascript
export function addMessage(role, content, createdAt = null, isStreaming = false) {
  // == 原有 DOM 创建逻辑保持不变 ==
  // ... (保留现有代码，确保现有 SSEResponser 路径不受影响)

  // == 新增：同步数据到 Alpine store ==
  const chatStore = Alpine.store('chats');
  const active = chatStore.active;
  if (active) {
    if (role === 'user') {
      const lastMsg = active.messages[active.messages.length - 1];
      const newId = lastMsg ? lastMsg.id + 1 : 1;
      active.messages.push({
        id: newId,
        role: 'user',
        content,
        createdAt: createdAt || undefined,
      });
    }
    // assistant 消息在流式结束时通过 chatStore.finalizeStreaming() 推入
  }

  // == 原有返回值不变 ==
  return div;
}
```

**验证方式**：
1. 发送消息 → 检查浏览器控制台 `Alpine.store('chats').active.messages` → 应有 user 消息
2. 流式完成后 → messages 中应有对应的 assistant 消息
3. 页面显示无变化（DOM 仍由 `addMessage()` 创建，Alpine 模板尚未激活）

---

### Phase C：SSEResponser → Alpine Store 数据流

**目标**：SSEResponser 不再直接操作 DOM，而是更新 `$store.chats`。Alpine 响应式系统自动驱动 DOM。

**核心变更**：将 SSEResponser 从 DOM 操作模式切换到数据操作模式。

#### C.1 简化 SSEResponser

修改 [`chat-sse-responser.js`](frontend/static/chat-sse-responser.js)：

```javascript
export class SSEResponser {
  constructor(session) {
    this.session = session;
  }

  get isActive() {
    return this.session._isActive;
  }

  /** 获取 Store 中对应的 chat 数据 */
  get chatData() {
    return Alpine.store('chats').getOrCreate(this.session.sn);
  }

  onReasoning(event) {
    this.session.streamingMsg.reasoning += event.content || '';
    // 同步到 Alpine store（如果是活跃 chat，Alpine 自动渲染）
    const chatData = this.chatData;
    if (chatData.streamingMsg) {
      chatData.streamingMsg.reasoning = this.session.streamingMsg.reasoning;
    }
  }

  onText(event) {
    this.session.streamingMsg.content += event.content || '';
    const chatData = this.chatData;
    if (chatData.streamingMsg) {
      chatData.streamingMsg.content = this.session.streamingMsg.content;
    }
  }

  onSources(event) {
    if (event.sources) {
      this.session.streamingMsg.sources.push(...event.sources);
    }
    if (event.web_sources) {
      this.session.streamingMsg.sources.push(...event.web_sources);
    }
    const chatData = this.chatData;
    if (chatData.streamingMsg) {
      chatData.streamingMsg.sources = [...this.session.streamingMsg.sources];
    }
  }

  onDone(event) {
    const msg = this.session.streamingMsg;
    msg.isDone = true;
    msg.msgId = event.msg_id || 0;
    msg.createdAt = event.created_at || null;
    msg.usage = event.usage || null;

    // finalize: streamingMsg → messages
    Alpine.store('chats').finalizeStreaming(this.session.sn);
  }

  onError(event) {
    this.session.streamingMsg.error = event.message || '未知错误';
    const chatData = this.chatData;
    if (chatData.streamingMsg) {
      chatData.streamingMsg.error = this.session.streamingMsg.error;
    }
  }

  /**
   * 当 session 从非活跃变为活跃时，将累积的 streamingMsg 同步到 Alpine store
   */
  flushToAlpine() {
    const chatData = this.chatData;
    if (chatData.streamingMsg) {
      // 数据已经通过上面的 onReasoning/onText 同步了
      // 只需要 Alpine 检测到 active 变化即可
    }
  }
}
```

**关键**：在 Phase C 初期，**SSEResponser 同时保留 DOM 操作路径和数据路径**（双写），确保平稳过渡。验证 `$store.chats` 数据正确后，再移除 DOM 操作。

#### C.2 改造 `prepareChat()` 使用 Alpine Store

修改 [`chat-sse.js`](frontend/static/chat-sse.js) 的 `prepareChat()`：

```javascript
function prepareChat() {
  // ... 现有验证逻辑 ...

  // 不再调用 addMessage('assistant', '', null, true) 创建 DOM
  // 改为通过 Alpine store 管理 streaming 状态
  const sn = state.currentChatSN;
  
  // 将用户消息写入 Alpine store
  const chatStore = Alpine.store('chats');
  chatStore.startStreaming(sn);
  // 用户消息已在 addUserMessage 中推入 state.messages
  // 后续通过 Phase B 写入 Alpine store

  // 获取 ChatSession（由 sessionManager 管理）
  const session = sessionManager.getOrCreate(sn);
  session.isStreaming = true;
  session._isActive = true;
  session.resetStreaming();
  // 不再设置 session.assistantBubble 和 session.contentDiv（DOM 由 Alpine 管理）

  // ...
}
```

**验证方式**：
1. 发送消息 → 流式内容出现（由 Alpine 模板渲染，不再是 `addMessage` 创建）
2. 切换到 chat B → chat A 的流式继续在后台，`$store.chats` 中 chat A 的 streamingMsg 持续更新
3. 切回 chat A → Alpine 自动显示已累积的内容

---

### Phase D：Per-chat isStreaming + UI 组件迁移（含过渡策略）

**目标**：所有 UI 组件从全局 `$store.settings.isStreaming` 改为 per-chat `$store.chats.active.isStreaming`。**最终彻底删除全局 isStreaming 代理层**。

**关键决策**：新架构中**不需要全局 isStreaming**。当前 `state.isStreaming`（[`chat-state.js:108`](frontend/static/chat-state.js:108)）形式上是个全局 getter，但实际已委托给 `sessionManager.getActive()?.isStreaming`——本质上已经是 per-chat 值。同理 `$store.settings.isStreaming` 也是全局的 proxy。

最终目标：**彻底删除**以下两层代理：
- `$store.settings.isStreaming`（从 [`alpine-store.js`](frontend/static/alpine-store.js) 删除）
- `state.isStreaming` getter/setter（从 [`chat-state.js`](frontend/static/chat-state.js) 删除）

所有读写路径统一到 `$store.chats.active.isStreaming`。

#### D.0 现状分析：三条 isStreaming 路径

```
路径 1: state.isStreaming (chat-state.js)
  → sessionManager.isStreaming → session.getActive()?.isStreaming
  → 用于 chat-sse.js / chat-ui.js 等 JS 代码的读写

路径 2: $store.settings.isStreaming (alpine-store.js)
  → 独立字段，由 applyStreamingState() 设置
  → 用于 Alpine UI 组件的 :disabled / :class 绑定

路径 3: ChatSession.isStreaming (chat-session.js)
  → per-chat 字段，由 prepareChat() / cleanupAfterStream() 设置
  → 这是真正的数据源
```

三条路径**互不同步**，靠代码维护一致性。重构的目标就是**合三为一**——路径 3 作为唯一真实源。

#### D.1 过渡期策略（Phase C 中同步执行）

在 Phase C 期间，逐步切换数据源：

**Step D.1a** — `$store.settings.isStreaming` 改为从 `$store.chats` 读取的 getter：

```javascript
// alpine-store.js — 过渡期
Alpine.store('settings', {
  // ... 保留 theme / deepThink / webSearch / sendMode 等字段 ...

  // isStreaming 改为 getter/setter，数据源为 $store.chats.active
  get isStreaming() {
    const chats = Alpine.store('chats');
    return chats.active ? chats.active.isStreaming : false;
  },
  set isStreaming(val) {
    const chats = Alpine.store('chats');
    if (chats.active) chats.active.isStreaming = val;
  },
});
```

**Step D.1b** — `state.isStreaming` 改为从 `$store.chats` 读取：

```javascript
// chat-state.js — 过渡期
get isStreaming() {
  try {
    const chats = window.Alpine.store('chats');
    return chats.active ? chats.active.isStreaming : false;
  } catch(e) { return false; }
},
set isStreaming(val) {
  try {
    const chats = window.Alpine.store('chats');
    if (chats.active) chats.active.isStreaming = val;
  } catch(e) {}
},
```

**Step D.1c** — `applyStreamingState()` 改为直接写 `$store.chats`：

```javascript
// chat-ui.js applyStreamingState — 过渡期
export function applyStreamingState(isStreaming) {
  // 改为直接写 $store.chats
  const chats = Alpine.store('chats');
  if (chats.active) {
    chats.active.isStreaming = isStreaming;
  }

  // 保持向后兼容：同时更新 ChatSession（session 路径）
  const activeSession = sessionManager.getActive();
  if (activeSession) {
    activeSession.isStreaming = isStreaming;
  }

  // 输入框禁用/启用（非 Alpine 元素）
  setInputEnabled(!isStreaming);

  // 删除按钮禁用（非 Alpine 元素）
  updateDeleteButtons();
}
```

**验证**：过渡期完成后，三条路径的数据源统一为 `$store.chats.active.isStreaming`，不存在不一致。

#### D.2 修改 UI 组件的 Alpine 绑定

在 [`index.html`](frontend/index.html) 中，将所有 `$store.settings.isStreaming` 替换为 `$store.chats.active?.isStreaming`：

| 当前绑定 | 改为 |
|---------|------|
| `:disabled="$store.settings.isStreaming"` | `:disabled="$store.chats.active?.isStreaming"` |
| `:class="{ streaming: $store.settings.isStreaming }"` | `:class="{ streaming: $store.chats.active?.isStreaming }"` |
| `active: () => $store.settings.isStreaming` | `active: () => $store.chats.active?.isStreaming` |

具体位置：

| 元素 | 行号 | 改动 |
|------|------|------|
| `input-area` `:class` | [`index.html:195`](frontend/index.html:195) | `$store.settings.isStreaming` → `$store.chats.active?.isStreaming` |
| `messageInput` `:disabled` | [`index.html:215`](frontend/index.html:215) | 同上 |
| `stopStreamingBtn` `:disabled` | [`index.html:226`](frontend/index.html:226) | 同上 |
| `sendBtn` `x-data` active | [`index.html:266`](frontend/index.html:266) | 同上 |
| `newChatBtn` `disabled` | [`index.html:126`](frontend/index.html:126) | 同上 |
| `aiTitleBtn` `disabled` | [`index.html:136`](frontend/index.html:136) | 同上 |
| `loginBtn` `disabled` | [`index.html:170`](frontend/index.html:170) | 同上 |

#### D.3 最终清理（所有绑定改完后执行）

**删除**以下无用的代理：

| 删除的内容 | 文件 | 说明 |
|-----------|------|------|
| `$store.settings.isStreaming`（含 get/set） | [`alpine-store.js`](frontend/static/alpine-store.js) | 不再需要全局代理 |
| `state.isStreaming` getter/setter | [`chat-state.js`](frontend/static/chat-state.js) | 不再需要状态委托 |
| `state.abortController` getter/setter | [`chat-state.js`](frontend/static/chat-state.js) | 已由 ChatSession 持有 |
| `state.messages` | [`chat-state.js`](frontend/static/chat-state.js) | 改为 `$store.chats.active.messages` |
| `state.currentChatSN` | [`chat-state.js`](frontend/static/chat-state.js) | 改为 `$store.chats.active.sn` |
| `state.dialogTitle` | [`chat-state.js`](frontend/static/chat-state.js) | 改为 `$store.chats.active.title` |
| `state.titleState` | [`chat-state.js`](frontend/static/chat-state.js) | 改为 `$store.chats.active.titleState` |
| `sessionManager.isStreaming` getter | [`chat-session-manager.js:138`](frontend/static/chat-session-manager.js:138) | 不再需要 |

**注意**：`chat-state.js` 不能整个删除，以下字段仍需保留：
- `state.userScrolledUp` — 用户是否手动滚动
- `state.deepThinkActive` / `state.webSearchActive` — 按钮状态委托
- `state.sessionDeepThinkingEnabled` — 流式锁定
- `state._wasAborted` — 中断标记

---

## 4. 文件改动清单

| 文件 | 阶段 | 改动类型 | 说明 |
|------|------|---------|------|
| [`alpine-store.js`](frontend/static/alpine-store.js) | A | 新增 `$store.chats` | 核心数据模型 |
| [`index.html`](frontend/index.html) | B | 修改 `#chatContainer` 模板 | Alpine x-for 消息渲染 |
| [`chat-ui.js`](frontend/static/chat-ui.js) | B | 修改 `addMessage()` | 新增数据同步到 Alpine store |
| [`chat-markdown.js`](frontend/static/chat-markdown.js) | B | 新增全局注册 | 注册 `AlpineRenderMarkdown` |
| [`chat-sse-responser.js`](frontend/static/chat-sse-responser.js) | C | 重写 | 从 DOM 操作改为 Alpine store 更新 |
| [`chat-sse.js`](frontend/static/chat-sse.js) | C | 修改 `prepareChat()` | 改为 Alpine store 管理 streaming |
| [`chat-state.js`](frontend/static/chat-state.js) | D | 逐步废弃字段 | 迁移到 Alpine store |
| [`index.html`](frontend/index.html) | D | 修改所有 UI 组件绑定 | `$store.settings.isStreaming` → `$store.chats.active?.isStreaming` |
| [`chat-restore.js`](frontend/static/chat-restore.js) | B/D | 修改 `restoreChat()` | 数据写入 Alpine store 而非 `state.messages` |
| [`chat-list.js`](frontend/static/chat-list.js) | D | 清理 | 移除对 `state.messages` 的引用 |
| [`chat-ticknav.js`](frontend/static/chat-ticknav.js) | P2 | 可选改造 | DOM 查询 → Alpine store 数据 |

---

## 5. 边界情况与风险

### 5.1 流式气泡的增量渲染

当前流式气泡使用 `throttleRender()` 节流 + `innerHTML` 增量渲染 Markdown。改用 Alpine 后，`streamingMsg.content` 的每次更新都会触发 Alpine 的响应式更新，可能导致：

- **频率过高**：SSE text 事件可能每帧到达多次，每次更新 `streamingMsg.content` 都会触发 Alpine 的 DOM diff
- **解决方案**：在 Alpine store 中增加 `_renderBuffer` 和节流机制，或在 SSEResponser 中节流写入

### 5.2 Alpine x-html 的安全风险

`x-html="AlpineRenderMarkdown(msg.content)"` 会插入原始 HTML。确保 `renderMarkdown()` 已处理 XSS（当前已使用 remarkable.js，默认安全）。

### 5.3 双写期的一致性

Phase B/C 过渡期，`addMessage()` 同时创建 DOM 和写入 Alpine store。需确保数据一致性：
- 页面刷新后，`restoreChat()` 只写 Alpine store
- 流式进行中，SSEResponser 同时写 DOM（旧路径）和 Alpine store（新路径）
- 过渡期结束后，移除 DOM 路径

### 5.4 刻度导航的改造

[`chat-ticknav.js`](frontend/static/chat-ticknav.js) 当前通过 `querySelectorAll('.message.user')` 查询 DOM 来构建刻度导航。改为从 `$store.chats.active.messages` 读取后，需要处理：
- 流式进行中，新 user 消息推入 messages → 自动增加刻度
- 删除消息组 → 自动减少刻度
- 这属于 P2 级别，可放在最后做

---

## 6. 验证清单

### Phase A 验证
- [ ] `Alpine.store('chats')` 存在且包含 items/activeIndex/active
- [ ] `getOrCreate()` 对同一 SN 返回同一对象
- [ ] `switchTo()` 正确改变 `activeIndex`
- [ ] `startStreaming()` / `finalizeStreaming()` 正确管理 streamingMsg 生命周期

### Phase B 验证
- [ ] `AlpineRenderMarkdown()` 在 Alpine 模板中可调用
- [ ] `addMessage()` 同时写入 Alpine store（数据路径可用）
- [ ] 现有 UI 无变化（DOM 仍由 addMessage 创建）

### Phase C 验证
- [ ] SSE 流式数据正确同步到 `$store.chats`
- [ ] 活跃 chat 的 streamingMsg 实时渲染
- [ ] 非活跃 chat 的 streamingMsg 在后台累积
- [ ] 切回非活跃 chat 时，已累积的 streamingMsg 正确恢复

### Phase D 验证
- [ ] 所有 UI 组件正确绑定 `$store.chats.active?.isStreaming`
- [ ] chat A 流式中切换到 chat B → chat B 的 UI 恢复正常（非流式）
- [ ] chat B 发送消息 → chat B 进入流式态
- [ ] 切回 chat A → chat A 仍显示流式态（或已完成态）
