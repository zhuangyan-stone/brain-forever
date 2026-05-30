# Alpine-first 重构计划

## 目标

消除项目中所有"非 Alpine-first"模式，使代码结构就像一开始就基于 Alpine.js 设计的一样。

## 审计结果：6 类需要重构的模式

```
当前脚本加载（三层）
  <script src="alpine-store.js">     ← 注册 Alpine store
  <script src="components/buttons.js"> ← 全局 window.iconBtn 等
  <script src="svg_icons.js">         ← 全局 window.ICON_XXX
  <script defer src="alpine.min.js">
  <script type="module" src="chat.js">

目标脚本加载（两层）
  <script src="/static/alpine-init.js"> ← 统一注册：store + data + 图标
  <script defer src="/static/alpine.min.js">
  <script type="module" src="app.js">
```

---

## 分步计划

### Phase 1: 合并组件注册 — buttons.js → alpine:init

**当前问题**：`frontend/static/components/buttons.js` 作为普通 `<script>` 加载，通过 `window.iconBtn`、`window.textBtn` 等全局函数暴露组件定义。

**目标**：将所有组件函数改为 `Alpine.data('iconBtn', ...)` 注册，合并到现有的 `alpine:init` 事件中。

**涉及文件**：
- `frontend/static/components/buttons.js` — 移除 `window.*`，改为 `Alpine.data()` 调用
- `frontend/static/alpine-store.js` — 在 `alpine:init` 中添加按钮组件的 `Alpine.data()` 注册
- `frontend/index.html` — 移除 `<script src="/static/components/buttons.js">` 标签
- `frontend/index.html` — 所有 `x-data="iconBtn(...)"` → `x-data="iconBtn({...})"`（使用方式不变，`Alpine.data` 同名即可）

**涉及 9 个组件**：
| 当前 `window.*` | 改为 |
|-----------------|------|
| `window.iconBtn(config)` | `Alpine.data('iconBtn', (config) => ({...}))` |
| `window.textBtn(config)` | `Alpine.data('textBtn', (config) => ({...}))` |
| `window.toggleBtn(config)` | `Alpine.data('toggleBtn', (config) => ({...}))` |
| `window.sendBtn(config)` | `Alpine.data('sendBtn', (config) => ({...}))` |
| `window.attachBtn()` | `Alpine.data('attachBtn', () => ({...}))` |
| `window.deleteDialog()` | `Alpine.data('deleteDialog', () => ({...}))` |
| `window.titleEditDialog()` | `Alpine.data('titleEditDialog', () => ({...}))` |
| `window.chatContainer()` | `Alpine.data('chatContainer', () => ({...}))` |
| `window.formatTime(isoStr)` | 放在 `Alpine.data('chatContainer', ...)` 的 methods 中，或改为 `Alpine.magic('formatTime', ...)` |

---

### Phase 2: 合并 SVG 图标 — svg_icons.js → alpine:init

**当前问题**：`frontend/static/svg_icons.js` 作为普通 `<script>` 加载，通过 `window.ICON_CLOSE` 等全局变量暴露图标常量。

**分析**：图标是**静态字面量**而非响应式状态，放在 `window` 上对 Alpine 而言已经足够。但如果要彻底 Alpine-first，可以：

**方案 A（推荐）**：将图标注册为 Alpine store 的子属性，同时保留 Alpine 模板中的直接引用。

实际上，最简单且符合 Alpine 哲学的方式是：**将图标放入 Alpine.store('icons')，同时保持 `window.ICON_XXX` 作为别名**（因为 `x-html="ICON_CLOSE"` 直接求值 window 变量）。

但更好的方式是**把 Alpine 模板改为 `x-html="$store.icons.ICON_CLOSE"`**——因为这样就完全通过 Alpine store 访问了。不过这涉及 13 处 HTML 修改。

**决策**：鉴于图标是静态数据，且 `x-html="ICON_CLOSE"` 比 `$store.icons.ICON_CLOSE` 更简洁，保留当前方式：
- `svg_icons.js` 保持普通 `<script>`，赋值到 `window`
- 但将其加载移到 `alpine-store.js` 之前或合并到 `alpine-init.js`

**涉及文件**：
- `frontend/static/svg_icons.js` — 无变化
- `frontend/index.html` — 保留 `<script src="/static/svg_icons.js">`

---

### Phase 3: 消除 window.__* 桥接函数

**当前问题**：有 4 个 `window.__*` 桥接，用于 Alpine 模板调用 JS 模块方法：

| 桥接 | 设置位置 | 使用位置 | 方案 |
|------|----------|----------|------|
| `window.__settingsStore` | `alpine-store.js:676` | `chat-sse.js:256`, `chat.js:36,67,571,594,608,625` | 直接 `import` 或在 module 中直接 `Alpine.store('settings')` |
| `window.__selectChat` | `chat-list.js:1014` | `index.html` Alpine 模板 `@click="window.__selectChat(chat.sn)"` | 改为 Alpine store 方法 `$store.chats.selectChat(sn)` |
| `window.__showContextMenu` | `chat-list.js:1015` | `index.html` Alpine 模板 `@click="window.__showContextMenu($event, chat)"` | 改为 Alpine store 方法或自定义事件 |
| `window._alpineRenderMarkdown` | `chat-markdown.js:309` | `alpine-store.js:527,600,623`, `chat-list.js:34,50,593` | ES module 之间直接 `import` |

**涉及文件**：
- `frontend/static/chat-list.js` — 将 `selectChat`、`showContextMenu` 注册为 Alpine store 的方法
- `frontend/static/chat-markdown.js` — 导出一个函数供 module import，而非 `window._alpineRenderMarkdown`
- `frontend/index.html` — 将 `@click="window.__selectChat(...)"` 改为 `@click="$store.chats.selectChat(...)"`
- `frontend/static/chat-sse.js`, `frontend/static/chat.js` — 用 `import` 代替 `window.__settingsStore`

---

### Phase 4: 将 tick-state.js 并入 Alpine Store

**当前问题**：`frontend/static/tick-state.js` 是模块级变量（`export let`），被 4 个文件 import。刻度导航是一个纯 UI 功能，其状态完全适合放在 Alpine store 中。

**涉及状态**：
| 变量 | 用途 |
|------|------|
| `activeTickIndex` | 当前活动刻度索引 |
| `tickScrollOffset` | 刻度列表滚动偏移 |
| `targetTickIndex` | 平滑滚动目标索引 |
| `pendingHighlightIndex` | 待高亮消息索引 |
| `MAX_VISIBLE_TICKS` | 常量，可以留在模块中 |

**方案**：在 Alpine store `chats` 中添加刻度相关状态，或新建一个独立的 `Alpine.store('tickNav', {...})`。

**涉及文件**（共 4 个 import 者）：
- `frontend/static/chat-ticknav.js` — 改为 `Alpine.store('tickNav')` 或组件 `x-data`
- `frontend/static/chat-list.js` — 改为 Alpine store 访问
- `frontend/static/chat.js` — 改为 Alpine store 访问
- `frontend/static/chat-api.js` — 仅 import `resetTickState`，改为 Alpine store 方法
- `frontend/static/components/buttons.js` — 已有 `window.__moduleTickNav` 引用

**注意**：`chat-ticknav.js` 是纯 DOM 操作（创建/更新刻度元素），即使状态在 Alpine store 中，其 DOM 操作逻辑保持不变。改为 Alpine store 后，`buttons.js` 中的刻度高亮调用可以简化为 `Alpine.store('tickNav').setActiveTick(idx)`。

---

### Phase 5: 简化脚本加载

**目标**：将 `index.html` 中 3 个普通 `<script>` 合并为 1 个。

```
当前：
  <script src="alpine-store.js">
  <script src="components/buttons.js">
  <script src="svg_icons.js">
  <script defer src="alpine.min.js">

目标：
  <script src="alpine-init.js">
  <script defer src="alpine.min.js">
```

**方案**：合并为 `alpine-init.js`，在 `alpine:init` 事件中完成所有注册：
- Alpine.store('settings', ...)
- Alpine.store('ui', ...)
- Alpine.store('chats', ...)
- Alpine.data('iconBtn', ...)
- Alpine.data('textBtn', ...)
- ... 所有组件
- `window.ICON_XXX = ...` 赋值（如需保留）

**涉及文件**：
- 🆕 `frontend/static/alpine-init.js` — 从 `alpine-store.js` + `buttons.js` + `svg_icons.js` 合并
- 🗑️ `frontend/static/alpine-store.js` — 删除
- 🗑️ `frontend/static/components/buttons.js` — 删除
- `frontend/index.html` — 3 个 `<script>` 替换为 1 个

---

### Phase 6: 移除 svg_icons_re.js（可选）

如果 Phase 5 完成后，`svg_icons.js` 在 `alpine-init.js` 之前加载，其 `window.ICON_XXX` 仍然对 module importers 可用，`svg_icons_re.js` 可以保留或删除。

如果保留：`svg_icons_re.js` 继续作为 ES module 的 re-export 层。
如果删除：所有 importer 改为直接引用 `window.ICON_XXX`（但破坏 module 模式）。

**建议**：保留 `svg_icons_re.js`，保持 ES module 导入路径清晰。

---

## 执行顺序总览

```
Phase 1: buttons.js → Alpine.data() 合并到 alpine:init
    ↓
Phase 2: svg_icons.js 保持现状（仅调整加载位置）
    ↓
Phase 3: 消除 window.__* 桥接
    ↓
Phase 4: tick-state.js → Alpine store
    ↓
Phase 5: 合并脚本加载 → alpine-init.js
    ↓
Phase 6: 清理验证
```

## 风险与注意事项

1. **Phase 1 风险最低**：`Alpine.data()` 注册和 `window.*` 全局函数在 HTML 中使用方式完全一致（`x-data="iconBtn(config)"`），只需确保 `Alpine.data` 在 Alpine 扫描 DOM 前注册。
2. **Phase 3 需谨慎**：`window.__selectChat` 和 `window.__showContextMenu` 在 Alpine 模板的 `@click` 表达式中使用，改为 `$store.chats.selectChat(sn)` 后需确保方法已注册到 store。
3. **Phase 4 需注意**：`chat-ticknav.js` 中的 `updateActiveTickOnScroll` 在 scroll 事件中同步读取 tick 状态，改为 Alpine store 后需要评估性能影响（Alpine 响应式可能会有微小延迟）。
4. **Phase 5 合并后**：确保 `alpine-init.js` 在 Alpine.js 之前加载且不含 `export`/`import` 关键字。
