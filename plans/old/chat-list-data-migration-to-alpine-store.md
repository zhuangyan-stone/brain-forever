# chat-list.js 数据迁入 Alpine Store 迁移计划

## 目标

消除 [`chat-list.js`](../frontend/static/chat-list.js) 中的两个模块状态变量：
- `currentChats`（原始对话列表）
- `activeChatSN`（当前选中对话 SN）

将它们迁入 Alpine store `chats`，使 Alpine store 成为**唯一的原始数据源**。

## 当前架构痛点

```
chat-list.js（模块变量）         Alpine store
  currentChats[]  ──→ renderChatList ──→ restructChatLists ──→ chatsTimeline[]
  activeChatSN    ──→ chats.activeChatSN
       ↑                               ↑
       └── 双向维护，可能不一致 ──────────┘

外部消费者通过 import 或动态 import 访问 chat-list.js：
  chat-sse-responser.js（currentChats + renderChatList）
  chat-api.js          （updateChatTitleBySN）
  chat-sse.js          （addDirtyChat + updateChatEntry）
  chat-ui.js           （updateCurrentChatTitle）
  chat.js              （clearActiveChat）
  chat-init.js         （renderChatList）
```

## 迁移后架构

```
Alpine store 'chats'
  chatList[]      ← 原始数据（NEW，替代 currentChats）
  activeChatSN    ← 选中状态（已存在，替代模块变量）
       │
       ├── restructChatLists() ──→ chatsTimeline[]（加工视图）
       └── 外部所有消费者直接读写 Alpine store
```

## 迁移步骤

### Step 1：Alpine store 新增 `chatList` 字段

**文件：** [`alpine-store.js`](../frontend/static/alpine-store.js)

- 在 [`Alpine.store('chats')`](../frontend/static/alpine-store.js:177) 的数据声明中添加：
  ```javascript
  chatList: [],    // 原始对话列表（NEW，替代 chat-list.js 的 currentChats）
  ```
- 在 [`resetToBlank()`](../frontend/static/alpine-store.js:215) 中添加 `this.chatList = []`
- 在 [`clearSidebar()`](../frontend/static/alpine-store.js:756) 中添加 `this.chatList = []`（虽然该方法目前未被调用，但保持一致性）
- **不修改** `restructChatLists` 的签名——它仍然接收 `chats` 参数，但新增一个逻辑：如果未传 `chats` 参数，则使用 `this.chatList`

### Step 2：更新 `restructChatLists` 支持从 `this.chatList` 读取

**文件：** [`alpine-store.js`](../frontend/static/alpine-store.js)

将 [`restructChatLists(chats, activeSN)`](../frontend/static/alpine-store.js:281) 改为：
```javascript
restructChatLists: function(chats, activeSN) {
    if (chats !== undefined) {
        this.chatList = chats;  // 同步保存到 chatList
    } else {
        chats = this.chatList;  // 从 chatList 读取
    }
    this.activeChatSN = activeSN || null;
    // ... 原有加工逻辑不变 ...
}
```

这样已有的调用方（如 `renderChatList`）传 `chats` 参数时也会同步更新 `chatList`，为逐步迁移提供向后兼容。

### Step 3：更新 `renderChatList` 改为写入 `chats.chatList`

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
export function renderChatList(chats, activeSN) {
    currentChats = chats || [];                    // ← 保留旧逻辑（临时）
    activeChatSN = activeSN || null;               // ← 保留旧逻辑（临时）
    
    closeContextMenu();
    
    try {
        var chatsStore = window.Alpine.store('chats');
        if (chatsStore) {
            chatsStore.restructChatLists(chats, activeSN);  // ← Step 2 会同步 chatList
        }
    } catch(e) {}
}
```

此时 `currentChats` 和 `activeChatSN` 仍是活跃的，但已经与 store 同步。这一步是安全的过渡状态。

### Step 4：迁移 `selectChat` 使用 Alpine store 作为数据源

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
async function selectChat(sn) {
    var chats = window.Alpine.store('chats');
    if (!chats) return;

    let hasChanged = chats.activeChatSN !== sn;     // ← 改用 store 的值
    if (hasChanged) chats.activeChatSN = sn;        // ← 只写 store

    // 同步到 Alpine store（侧边栏高亮 + 关闭抽屉）
    // ★ 不再需要第二个条件检查，因为 hasChanged 基于 store 判断
    if (hasChanged) {
        chats.activeChatSN = activeChatSN;           // ← 简化
    }
    // ... 后续逻辑不变 ...
}
```

**关键变化：** `let hasChanged = chats.activeChatSN !== sn` 替代原来的 `activeChatSN !== sn`。不再需要第二个防御性条件，因为检查的是同一个 store 值。

### Step 5：迁移 `clearActiveChat` 只写 Alpine store

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
export function clearActiveChat() {
    try {
        var chatsStore = window.Alpine.store('chats');
        if (chatsStore) {
            chatsStore.activeChatSN = null;          // ← 只需写 store
        }
    } catch(e) {}
}
```

模块变量 `activeChatSN = null` 不再需要。

**文件：** [`chat.js`](../frontend/static/chat.js:151)
调用方 `clearActiveChat()` 无需改动，因为 export 的函数签名不变。

### Step 6：迁移 `addDirtyChat` 操作 `chats.chatList`

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
export function addDirtyChat(title, sn) {
    if (!sn) return;

    var chatsStore = window.Alpine.store('chats');
    if (!chatsStore) return;

    const chatList = chatsStore.chatList;            // ← 改用 store 的 chatList
    
    const existing = chatList.find(c => c.sn === sn);
    if (existing) {
        if (title) existing.title = title;
        chatsStore.activeChatSN = sn;                // ← 直接写 store
        renderChatList(chatList, sn);
        return;
    }

    const dirtyChat = {
        id: 0,
        sn: sn,
        title: title,
        title_state: 0,
        pinned: false,
        category: 0,
        role_no: 0,
        create_at: new Date().toISOString(),
        update_at: new Date().toISOString(),
    };

    chatList.unshift(dirtyChat);
    chatsStore.activeChatSN = sn;
    renderChatList(chatList, sn);
}
```

### Step 7：迁移 `updateCurrentChatTitle` 操作 `chats.chatList`

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
export function updateCurrentChatTitle(newTitle) {
    if (!newTitle) return;

    var chatsStore = window.Alpine.store('chats');
    if (!chatsStore) return;

    const sn = chatsStore.activeChatSN;              // ← 从 store 读取
    if (!sn) {
        // 新对话刚创建后 activeChatSN 为 null（clearActiveChat 清除了选中状态），
        // 此时 chatList 中最后一个（最旧的）对话可能有一个空标题或默认标题的占位。
        // 但我们无法精确匹配到新对话，因此直接跳过 DOM 更新，
        // 等后续 updateChatEntry 拿到真实 SN 后会补充更新标题。
        return;
    }

    // 更新内存中的标题
    const chat = chatsStore.chatList.find(c => c.sn === sn);
    if (chat) {
        chat.title = newTitle;
    }

    // 更新 DOM
    const chatItem = document.querySelector(`.chat-item[data-sn="${CSS.escape(sn)}"]`);
    if (chatItem) {
        const titleEl = chatItem.querySelector('.chat-title');
        if (titleEl) {
            titleEl.textContent = truncateTitle(newTitle);
        }
    }
}
```

### Step 8：迁移 `updateChatTitleBySN` 操作 `chats.chatList`

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
export function updateChatTitleBySN(sn, newTitle) {
    if (!sn || !newTitle) return;

    var chatsStore = window.Alpine.store('chats');
    if (!chatsStore) return;

    const chat = chatsStore.chatList.find(c => c.sn === sn);
    if (!chat) {
        return;  // chat 已被删除，静默跳过
    }

    chat.title = newTitle;
    renderChatList(chatsStore.chatList, chatsStore.activeChatSN);
}
```

### Step 9：迁移 `updateChatEntry` 操作 `chats.chatList`

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
export function updateChatEntry(sn, title, titleState) {
    if (!sn) return;

    var chatsStore = window.Alpine.store('chats');
    if (!chatsStore) return;

    const chatList = chatsStore.chatList;

    // 检查该 SN 是否已存在
    const existing = chatList.find(c => c.sn === sn);
    if (existing) {
        if (title !== undefined) existing.title = title;
        if (titleState !== undefined) existing.title_state = titleState;
    } else {
        // 不存在：移除脏数据（sn=null 的占位条目），然后添加真实条目
        chatsStore.chatList = chatList.filter(c => c.sn !== null);
        
        const newChat = {
            id: 0,
            sn: sn,
            title: title || '',
            title_state: titleState || 0,
            pinned: false,
            category: 0,
            role_no: 0,
            create_at: now,
            update_at: now,
        };
        chatsStore.chatList.unshift(newChat);
    }

    renderChatList(chatsStore.chatList, chatsStore.activeChatSN);
}
```

### Step 10：迁移 `handleDelete` 操作 `chats.chatList`

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
async function handleDelete(chat) {
    // ... API 调用逻辑不变 ...

    // 从本地数据移除
    var chatsStore = window.Alpine.store('chats');
    if (chatsStore) {
        const idx = chatsStore.chatList.findIndex(c => c.sn === chat.sn);
        if (idx >= 0) {
            chatsStore.chatList.splice(idx, 1);
        }
        
        // 从 Alpine store 的 items[] 中同步移除 ChatData
        chatsStore.removeChat(chat.sn);

        // 如果删除的是当前活动对话
        if (chatsStore.activeChatSN === chat.sn) {
            // ... DOM 清理逻辑不变 ...
            chatsStore.activeChatSN = null;          // ← 只写 store
            showWelcomeMessage();
        }

        renderChatList(chatsStore.chatList, chatsStore.activeChatSN);
    }
    // ...
}
```

### Step 11：迁移 `setSidebarChats` 操作 `chats.chatList`

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

```javascript
chats.setSidebarChats = function(chatList, activeSN) {
    closeContextMenu();
    // 直接调用 Alpine store 的 restructChatLists
    chats.restructChatLists(chatList, activeSN);
};
```

**不再需要**维护模块变量 `currentChats` 和 `activeChatSN`。

### Step 12：移除 SSE responser 中的 `__chatListModule` 死代码

**文件：** [`chat-sse-responser.js`](../frontend/static/chat-sse-responser.js)

将 [`onChatCreated`](../frontend/static/chat-sse-responser.js:220) 中的：
```javascript
// 3. 更新侧边栏 currentChats 中的 SN
try {
    var chatListModule = window.__chatListModule;
    if (!chatListModule) {
        import('./chat-list.js').then(function(mod) {
            var currentChats = mod.currentChats;
            ...
        });
    } else {
        var currentChats = chatListModule.currentChats;
        ...
    }
} catch(e) {}

if (chats.activeChatSN === frontSN) {
    chats.activeChatSN = event.sn;
}
```

改为直接操作 Alpine store：
```javascript
// 3. 更新侧边栏 chatList 中的 SN
try {
    var currentChats = chats.chatList;
    if (currentChats) {
        var chatIdx = currentChats.findIndex(function(c) { return c.sn === frontSN; });
        if (chatIdx >= 0) {
            currentChats[chatIdx].sn = event.sn;
            chats.restructChatLists(currentChats, chats.activeChatSN);
        }
    }
} catch(e) {}
```

注意：这里不再需要单独设置 `chats.activeChatSN = event.sn`，因为 `restructChatLists` 内部已处理。

### Step 13：清理 `chat-list.js` 中的模块变量声明

**文件：** [`chat-list.js`](../frontend/static/chat-list.js)

- 移除 `export let currentChats = []`（第 49 行）
- 移除 `let activeChatSN = null`（第 50 行）
- 检查所有内部函数是否还有对这两个变量的直接引用，确认全部已迁移

### Step 14：更新 `chat-init.js` 注释

**文件：** [`chat-init.js`](../frontend/static/chat-init.js)

更新 [`initPage`](../frontend/static/chat-init.js:20) 中的注释，说明不再需要确保 `currentChats` 变量初始化，因为数据直接进入 Alpine store。

## 回滚策略

每个步骤都是可逆的：
- 如果 Step 3-10 中某个函数出问题，可以暂时回退到模块变量版本
- `renderChatList` 的旧逻辑（写模块变量）可以保留作为过渡，待全部迁移完成后再移除
- Step 12 是独立的，可以先做，也可以最后做

## 风险点

| 风险 | 缓解措施 |
|---|---|
| Alpine store 时序问题（模块初始化时 store 不可用） | Step 4 中 `selectChat` 已有 `if (!chats) return` 守卫；其他函数也添加类似守卫 |
| `chats.chatList` 非响应式变更导致 Alpine 模板不更新 | `restructChatLists` 会重新生成 `chatsTimeline`，触发 Alpine 响应式更新 |
| 引用 `currentChats` 的外部模块未同步更新 | Step 12 专门处理 SSE responser；其他外部消费者通过函数调用而非直接访问变量 |

## 受影响的文件和修改摘要

| 文件 | 修改内容 |
|---|---|
| [`alpine-store.js`](../frontend/static/alpine-store.js) | 新增 `chatList` 字段；`resetToBlank`/`clearSidebar` 同步重置；`restructChatLists` 支持从 `this.chatList` 读取 |
| [`chat-list.js`](../frontend/static/chat-list.js) | 所有内部函数改为读取 `chats.chatList` 和 `chats.activeChatSN`；移除模块变量声明 |
| [`chat-sse-responser.js`](../frontend/static/chat-sse-responser.js) | 移除 `__chatListModule` 分支，直接操作 `chats.chatList` |
| [`chat-init.js`](../frontend/static/chat-init.js) | 更新注释（无逻辑变更） |
