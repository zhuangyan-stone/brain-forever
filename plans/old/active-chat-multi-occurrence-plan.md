# 侧边栏 Active Chat 多实例高亮方案 — 执行计划

## 最终方案：主次样式区分（方案 C）

### 行为规则

1. 用户在 **分类 tab** 点击某个 chat 时，点击来源的那个 `.chat-item` 显示完整 `.active` 样式（蓝色左边框 + accent 色），其他出现位置显示淡色 `.active-sub` 样式
2. 切换到 **时间线 tab** 时，该 chat 在时间线中显示完整 `.active`（chat 只出现一次，无歧义）
3. 从时间线切回分类 tab 时，**所有出现位置都显示 `.active-sub`（淡色）**，用户需要重新点击一个具体条目才能恢复完整高亮
4. 时间线 tab 不需要 `.active-sub`

### 涉及文件

| 文件 | 改动类型 |
|------|----------|
| [`frontend/static/alpine-store.js`](frontend/static/alpine-store.js) | 新增 store 字段 + 方法 |
| [`frontend/static/chat-list.css`](frontend/static/chat-list.css) | 新增 `.active-sub` 样式 |
| [`frontend/index.html`](frontend/index.html) | 模板添加 `active-sub` 条件 |
| [`frontend/static/chat-list.js`](frontend/static/chat-list.js) | `selectChat` 传参 |

---

### Step 1：Alpine Store — 新增字段和方法

**文件：** [`frontend/static/alpine-store.js`](frontend/static/alpine-store.js)

在 `Alpine.store('chats', { ... })` 的 data 区新增字段：

```js
// 追踪点击来源
activeChatSource: null,   // 'timeline' | 'favorites' | 'category' | null
activeSubSource: null,    // 区间内具体分组标识，如 'fav_工作' | 'cat_技术' | null
```

新增方法 `getActiveStyle(sn, section, subKey)`，供模板调用：

```js
/**
 * 判断指定 chat-item 应该使用哪种 active 样式
 * @param {string} sn - 对话 SN
 * @param {'timeline'|'favorites'|'category'} section - 所在区间
 * @param {string} [subKey] - 区间内分组标识（收藏栏传 custom_tag，分类传 tag）
 * @returns {'active'|'active-sub'|null}
 */
getActiveStyle(sn, section, subKey) {
    if (sn !== this.activeChatSN) return null;
    
    // 时间线 tab — 只有一个实例，用完整 active
    if (section === 'timeline') return 'active';
    
    // 分类 tab
    if (this.activeChatSource === section && this.activeSubSource === subKey) {
        // 来源匹配 → 完整 active
        return 'active';
    }
    
    // 其他出现位置 → 淡色 active-sub
    return 'active-sub';
},
```

在 `resetToBlank()` 和 `reset()` 中重置新字段：

```js
resetToBlank: function() {
    // ... 现有代码 ...
    this.activeChatSource = null;
    this.activeSubSource = null;
},

reset: function() {
    // ... 现有代码 ...
    this.activeChatSource = null;
    this.activeSubSource = null;
},
```

在 `switchSidebarTab` 中：切换到分类 tab 时，如果 `activeChatSource !== 'favorites' && activeChatSource !== 'category'`（即来自时间线），清空 `activeChatSource` 使所有 item 变为 `active-sub`：

```js
switchSidebarTab: function(tab) {
    this.sidebarTab = tab;
    if (tab === 'category') {
        // 从时间线切过来 → 降级为 active-sub
        if (this.activeChatSource !== 'favorites' && this.activeChatSource !== 'category') {
            this.activeChatSource = null;
            this.activeSubSource = null;
        }
        // ... 现有加载逻辑 ...
    } else if (tab === 'timeline') {
        // 从分类切到时间线 → 保持 activeChatSN，时间线只用 active（无歧义）
    }
},
```

---

### Step 2：CSS — 新增 `.active-sub` 样式

**文件：** [`frontend/static/chat-list.css`](frontend/static/chat-list.css)

在 `.chat-item.active` 样式块之后新增：

```css
/* ---------- 次级活跃状态（同一 chat 多分类时的非主要高亮） ---------- */
.chat-item.active-sub {
    background: var(--bg-user);
    /* 无左边框 */
    opacity: 0.75;
}

.chat-item.active-sub .chat-item-title {
    color: var(--accent);
    font-weight: 400;
}
```

设计意图：
- 有背景色（和 `:hover` 一致），让用户感知"这个 chat 是当前的"
- **无**蓝色左边框，视觉上明显弱于主 `.active`
- 标题用 accent 色但不用加粗，降低视觉权重
- 轻微降低透明度进一步弱化

---

### Step 3：模板 — 添加条件判断

**文件：** [`frontend/index.html`](frontend/index.html)

#### 3a. 时间线 tab（line 181）

原代码：
```html
:class="{ active: chat.sn === $store.chats.activeChatSN }"
```

改为（逻辑不变，时间线只用 active）：
```html
:class="$store.chats.getActiveStyle(chat.sn, 'timeline') === 'active' ? 'chat-item active' : 'chat-item'"
```

> 注意：`chat-item` 基础 class 也需要保留。Alpine 的 `:class` 会覆盖静态 `class`，所以需要把 `chat-item` 移入动态表达式或使用 `x-bind:class` 合并。

更简洁的方式——保持静态 class 不变，用 `:class` 只处理 active 部分：

```html
:class="{ active: $store.chats.getActiveStyle(chat.sn, 'timeline') === 'active' }"
```

因为 `getActiveStyle` 返回 `'active' | null`，这里判断是否等于 `'active'` 即可。

#### 3b. 收藏栏（line 329-330）

原代码：
```html
:class="{ active: chat.sn === $store.chats.activeChatSN }"
```

改为：
```html
:class="{
    active: $store.chats.getActiveStyle(chat.sn, 'favorites', customTag) === 'active',
    'active-sub': $store.chats.getActiveStyle(chat.sn, 'favorites', customTag) === 'active-sub'
}"
```

#### 3c. 智能分类（line 374-375）

原代码：
```html
:class="{ active: chat.sn === $store.chats.activeChatSN }"
```

改为：
```html
:class="{
    active: $store.chats.getActiveStyle(chat.sn, 'category', tag) === 'active',
    'active-sub': $store.chats.getActiveStyle(chat.sn, 'category', tag) === 'active-sub'
}"
```

#### 3d. 更早分组的子分组 items（line 230-231）

同时间线逻辑（chat 只在 timeline tab 的子分组中出现一次），改为：
```html
:class="{ active: $store.chats.getActiveStyle(chat.sn, 'timeline') === 'active' }"
```

---

### Step 4：selectChat — 传递来源参数

**文件：** [`frontend/static/chat-list.js`](frontend/static/chat-list.js)

#### 4a. 修改 `selectChat` 函数签名（line 152）

```js
async function selectChat(sn, source, subSource) {
```

在函数开头设置 store 字段：

```js
async function selectChat(sn, source, subSource) {
    var chats = window.Alpine.store('chats');
    if (!chats) return;

    // 记录点击来源
    chats.activeChatSource = source || null;
    chats.activeSubSource = subSource || null;

    // ... 原有逻辑 ...
```

#### 4b. 模板中调用处传参

时间线 tab（index.html line 184）：
```html
@click="$store.chats.selectChat(chat.sn, 'timeline')"
```

收藏栏（index.html line 332）：
```html
@click="$store.chats.selectChat(chat.sn, 'favorites', customTag)"
```

智能分类（index.html line 377）：
```html
@click="$store.chats.selectChat(chat.sn, 'category', tag)"
```

---

### 边界情况处理

1. **切换 tab 时不重置 `activeChatSN`**：只重置 `activeChatSource`/`activeSubSource`，使所有分类 tab 内的 item 变为 `active-sub`，同一个 chat 仍处于选中状态
2. **清除选中（`clearActiveChat`）**：在 [`chat-list.js:clearActiveChat`](frontend/static/chat-list.js:62) 中一并重置新字段
3. **点击新对话**：`resetToBlank` 已覆盖新字段重置
4. **删除当前活跃 chat**：`handleDelete` 中的重置逻辑已覆盖

### 改动量预估

| 文件 | 新增/修改行数 |
|------|-------------|
| alpine-store.js | ~20 行 |
| chat-list.css | ~10 行 |
| index.html | ~15 行 |
| chat-list.js | ~5 行 |
| **合计** | **~50 行** |

---

### 效果演示

```
分类 Tab 视图：
┌─ 📌 收藏 ───────────────────┐
│ ▼ 工作 (2)                   │
│   ● 项目复盘    ← active     │  ← 点击来源，完整高亮（蓝色左边框）
│   ○ 周报汇总    ← active-sub │  ← 同一 chat 的其他 tag（浅色）
│ ▼ 学习 (1)                   │
│   ○ 项目复盘    ← active-sub │  ← 同一 chat 被收藏到多个目录
├─ 🤖 智能分类 ───────────────│
│ ▼ 技术 (1)                   │
│   ○ 项目复盘    ← active-sub │  ← 同一 chat 在 AI 分类中也出现
└──────────────────────────────┘
```
