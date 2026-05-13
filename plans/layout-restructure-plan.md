# 前端布局重构计划

## 目标

将当前项目前端布局改造为"左栏 + 主栏"flex 双栏弹性布局，参照 [`html_demo/demo.html`](html_demo/demo.html) 的实现方式。

**核心约束：**
- ✅ 消息历史区域（`#chatContainer` 内的内容）**完全不动**
- ✅ 输入面板区域（`.input-area` 及其子元素）**完全不动**
- ✅ 视觉风格保持现有 CSS 变量和样式定义
- ✅ 仅改造外层布局容器结构

---

## 1. 当前布局 vs 目标布局

### 当前布局

```
body (flex, justify-content:center, overflow:hidden)
├── .left-panel (position:fixed, width:0→380px)     ← 脱离文档流
├── .floating-header (position:fixed)                ← 脱离文档流
├── .layout-center (flex:1, max-width:1000px)
│   ├── #app (flex column)
│   │   ├── .header (标题栏)
│   │   ├── .chat-container#chatContainer (聊天区, overflow-y:auto)  ← 直接滚动
│   │   ├── .input-area (输入区)
│   │   └── .modal-overlay (模态框)
│   └── .tick-nav (刻度导航)
└── .right-panel (预留)
```

### 目标布局

```
body (overflow:hidden, height:100%)
└── .app-container (flex row, width:100%, height:100%)
    ├── .left-sidebar (width:400px, hidden class 控制显隐)
    │   ├── .sidebar-header (标题区, min-height:72px)
    │   │   └── .left-brand-area (Logo+标题+切换按钮, 动态渲染)
    │   └── .sidebar-content (flex:1, overflow-y:auto, 独立滚动)
    │       └── [左侧内容: 新对话按钮等]
    │
    └── .main-content (flex:1, flex column)
        ├── .main-header (标题区, min-height:72px)
        │   ├── .main-brand-area (左栏隐藏时显示Logo+标题+切换按钮)
        │   ├── .main-title (内容标题, 即 #headerTitle)
        │   └── .main-header-right (主题切换按钮)
        │
        ├── .main-body (flex:1, overflow:hidden, flex, justify-content:center)
        │   └── .scroll-container (width:80%, height:100%, overflow-y:auto)
        │       └── #chatContainer (原聊天容器, 保持原有样式不变)
        │
        └── .input-area (原样保留, 位置调整到 .main-content 底部)
```

---

## 2. 品牌区域样式定义

品牌区域由以下元素组成（复用现有 CSS 样式）：

```
品牌容器 (.brand-container)
├── <img class="brand-logo" src="/static/brain-forever.svg">   ← 现有 .left-panel-logo 样式
├── .brand-text
│   ├── <h1 class="brand-title">脑力永生</h1>                   ← 现有 .left-panel-title h1 样式
│   └── <p class="brand-subtitle">基于 RAG 知识库的智能对话</p>  ← 现有 .left-panel-title .subtitle 样式
└── <button class="menu-toggle-btn">☰</button>                 ← 切换按钮
```

**复用的 CSS class（来自现有 layout.css）：**

| 元素 | CSS class | 关键样式 |
|------|-----------|---------|
| Logo img | `.brand-logo`（复用 `.left-panel-logo`） | `width:38px; height:38px; flex-shrink:0` |
| 主标题 h1 | `.brand-title`（复用 `.left-panel-title h1`） | `font-size:1.2rem; font-weight:600; color:var(--text-primary)` |
| 副标题 p | `.brand-subtitle`（复用 `.left-panel-title .subtitle`） | `font-size:0.78rem; color:var(--text-muted)` |
| 切换按钮 | `.menu-toggle-btn`（新增，参照 demo） | `width:40px; height:40px; border-radius:40px; font-size:1.6rem` |

**品牌迁移逻辑（JS 动态渲染）：**
```
if (侧边栏对用户可见):
   品牌容器 → 渲染到 leftBrandContainer（左栏标题区）
   mainBrandContainer 清空
else:
   品牌容器 → 渲染到 mainBrandContainer（主栏标题区）
   leftBrandContainer 清空
```

---

## 3. 关键设计决策

| 决策 | 方案 | 理由 |
|------|------|------|
| `#chatContainer` 处理 | 保留 ID 和所有内部逻辑，放入新的 `.scroll-container` 内 | 消息区域完全不动 |
| `.input-area` 处理 | 保留所有 HTML 结构和 CSS class，移到 `.main-content` 底部 | 输入区域完全不动 |
| 品牌迁移 | JS 动态渲染，复用现有 CSS class | 灵活，不修改现有 CSS |
| 左栏显隐 | 宽屏用 `hidden` class，小屏用 `drawer-open` + `transform` | 与 demo 一致 |
| 标题高度一致 | 相同 `min-height` + flex 居中 | 简单可靠 |
| 滚动条位置 | 80% 宽滚动容器，滚动条在容器右边缘 | 需求 #7 |

---

## 4. 需修改的文件及具体变更

### 文件 1: [`frontend/index.html`](frontend/index.html) — 重写 body 结构

**保留不变的部分（原样复制）：**
- `<head>` 全部内容（CSS/JS 引用、内联脚本）
- `#chatContainer` 元素（仅移动位置，不改内容）
- `.input-area` 全部内容（仅移动位置，不改内容）
- `#toastContainer`、`#deleteModal`、`#tickNav`（提升到 body 级别）
- `<script type="module" src="/static/chat.js">`

**移除的部分：**
- `.left-panel`（替换为新的 `.left-sidebar`）
- `.floating-header`（由品牌迁移逻辑替代）
- `.layout-center` 和 `#app` 包装层
- `.right-panel`
- `.header`（替换为新的 `.main-header`）

**新增的结构：**
```html
<div class="app-container" id="appContainer">
  <!-- 左栏 -->
  <aside class="left-sidebar hidden" id="leftSidebar">
    <div class="sidebar-header">
      <div class="left-brand-area" id="leftBrandContainer"></div>
      <button class="sidebar-close-btn" id="sidebarCloseBtn">✕</button>
    </div>
    <div class="sidebar-content" id="sidebarContent">
      <button class="new-session-btn" id="newSessionBtn">...</button>
    </div>
  </aside>

  <!-- 主栏 -->
  <main class="main-content">
    <div class="main-header">
      <div class="main-brand-area" id="mainBrandContainer"></div>
      <span class="main-title" id="headerTitle">欢迎开始新对话</span>
      <div class="main-header-right">
        <button id="themeToggle" class="theme-toggle">...</button>
      </div>
    </div>
    <div class="main-body">
      <div class="scroll-container" id="scrollContainer">
        <!-- ★ 原 #chatContainer 原样放入此处 ★ -->
        <main class="chat-container" id="chatContainer"></main>
      </div>
    </div>
    <!-- ★ 原 .input-area 原样放入此处 ★ -->
    <footer class="input-area">...</footer>
  </main>
</div>
```

### 文件 2: [`frontend/static/layout.css`](frontend/static/layout.css) — 重写外层布局

**保留的样式（不动）：**
- `.chat-container` 及其滚动条样式（消息区域）
- `.message-group`、`.message`、`.bubble` 等消息样式
- `.welcome-message` 及其相关样式
- `.right-panel`
- `.left-panel-logo`、`.left-panel-title h1`、`.left-panel-title .subtitle`（品牌样式，复用）

**移除的样式：**
- `body` 的 flex 居中布局
- `.layout-center`、`#app` 相关
- `.floating-header`、`.left-panel`、`.left-panel-visible` 相关
- `.header`、`.header-left`、`.header-right`、`.header-menu-btn`、`.header-title`
- `.panel-toggle`、`.theme-toggle`（移到新位置或保留通用样式）

**新增的样式（参照 demo）：**
```css
/* 全局禁用滚动 */
html, body { overflow: hidden; height: 100%; margin: 0; padding: 0; }
* { box-sizing: border-box; }

/* 主容器 */
.app-container { display: flex; width: 100%; height: 100%; overflow: hidden; }

/* 左栏 */
.left-sidebar {
  width: 400px; flex-shrink: 0;
  display: flex; flex-direction: column; height: 100%; overflow: hidden;
  background: var(--bg-primary);
  border-right: 1px solid var(--border);
  transition: transform 0.2s ease;
}
.left-sidebar.hidden { display: none; }

.sidebar-header {
  flex-shrink: 0; min-height: 72px;
  display: flex; align-items: center;
  padding: 0.75rem 1.25rem;
  border-bottom: 1px solid var(--border);
}

.sidebar-content { flex: 1; overflow-y: auto; padding: 1rem; }

/* 主栏 */
.main-content {
  flex: 1; min-width: 400px;
  display: flex; flex-direction: column; height: 100%; overflow: hidden;
  background: var(--bg-primary);
}

.main-header {
  display: flex; align-items: center; gap: 1rem;
  padding: 1rem 1.5rem;
  border-bottom: 2px solid var(--border);
  flex-shrink: 0; min-height: 72px;
}

.main-body {
  flex: 1; min-height: 0; overflow: hidden;
  display: flex; justify-content: center; align-items: flex-start;
}

/* 80% 滚动容器 */
.scroll-container {
  width: 80%; height: 100%;
  overflow-y: auto; overflow-x: hidden;
}

/* 品牌区域容器 */
.main-brand-area, .left-brand-area {
  display: flex; align-items: center; gap: 12px;
}

.main-title {
  font-size: 0.95rem; font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  min-width: 0; user-select: none;
}

.main-header-right {
  margin-left: auto;
  display: flex; align-items: center; gap: 8px; flex-shrink: 0;
}

/* 切换按钮（参照 demo） */
.menu-toggle-btn {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  font-size: 1.6rem;
  width: 40px; height: 40px;
  border-radius: 40px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: 0.2s;
  flex-shrink: 0;
}
.menu-toggle-btn:hover { background: var(--bg-hover); }

.sidebar-close-btn {
  background: rgba(0,0,0,0.05);
  border: none;
  font-size: 1.4rem;
  width: 36px; height: 36px;
  border-radius: 40px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
```

### 文件 3: [`frontend/static/responsive.css`](frontend/static/responsive.css) — 重写

**移除：** 所有旧的响应式规则

**新增（参照 demo 的小屏抽屉模式）：**
```css
/* 小屏抽屉模式 */
body.small-screen-mode .app-container { position: relative; }
body.small-screen-mode .left-sidebar {
  position: fixed; top: 0; left: 0; width: 100%; height: 100%;
  z-index: 1050; background: var(--bg-primary);
  transform: translateX(-100%);
  transition: transform 0.25s cubic-bezier(0.2, 0.9, 0.4, 1.1);
  display: flex !important;
}
body.small-screen-mode .left-sidebar.drawer-open { transform: translateX(0); }
body.small-screen-mode .left-sidebar.hidden { display: flex !important; }
body.small-screen-mode .main-content { min-width: auto; width: 100%; }
body.small-screen-mode .sidebar-close-btn { display: inline-flex; }
body:not(.small-screen-mode) .sidebar-close-btn { display: none; }

@media (max-width: 768px) {
  .scroll-container { width: 95%; }
  .main-header, .sidebar-header { min-height: 64px; padding: 0.75rem 1rem; }
}
```

### 文件 4: [`frontend/static/chat.js`](frontend/static/chat.js) — 修改面板切换逻辑

**保留不变的部分：**
- 所有主题切换逻辑
- 所有按钮状态逻辑（深度思考、智能搜索）
- 输入区域逻辑（发送模式、键盘事件、附件）
- 新对话按钮逻辑
- 初始化调用（`initTickNav`、`initCopyHandlers`、`initDeleteModal`、`restoreSession`）
- 输入面板自动折叠逻辑（但需将 scroll 监听从 `#chatContainer` 改为 `#scrollContainer`）

**替换的部分：**
- 移除 `panelToggle`、`panelToggleInner`、`headerMenuBtn` 的旧切换逻辑
- 移除 `toggleLeftPanel()` 函数
- 新增完整的切换逻辑（参照 demo 的 JavaScript），包括：
  - 状态管理：`isLeftVisible`、`isSmallMode`、`isDrawerOpen`
  - 品牌创建函数：`createBrandElement()` — 生成 Logo img + 主标题 h1 + 副标题 p
  - 切换按钮单例：`getToggleButton()`
  - 统一切换入口：`toggleSidebarMaster()`
  - 小屏抽屉：`openDrawer()` / `closeDrawer()`
  - 宽屏显隐：`hideByUser()` / `attemptShow()`
  - 品牌迁移核心：`updateBrandLayout()`
  - 模式切换：`switchMode()`（resize 监听）
  - 初始化：`init()`

### 文件 5: [`frontend/static/chat-ui.js`](frontend/static/chat-ui.js) — 微调

**变更点：**
- `scrollToBottom()` 中的滚动目标从 `dom.chatContainer` 改为 `#scrollContainer`
- 在 `initDom()` 中增加 `dom.scrollContainer = document.getElementById('scrollContainer')`

---

## 5. 实施步骤（按顺序）

| 步骤 | 文件 | 操作 | 说明 |
|------|------|------|------|
| 1 | `frontend/index.html` | 重写 body 结构 | 新建 `.app-container`，将 `#chatContainer` 和 `.input-area` 原样移入 |
| 2 | `frontend/static/layout.css` | 重写外层布局 | 保留消息/输入/品牌样式，新增 flex 双栏 + 80% 滚动容器样式 |
| 3 | `frontend/static/responsive.css` | 重写 | 替换为小屏抽屉模式规则 |
| 4 | `frontend/static/chat.js` | 替换切换逻辑 | 移除旧 `toggleLeftPanel`，新增完整切换 + 品牌迁移逻辑 |
| 5 | `frontend/static/chat-ui.js` | 微调 | 更新滚动引用指向 `#scrollContainer` |

---

## 6. 注意事项

1. **`#chatContainer` 的滚动属性**：当前 `chat-container` 有 `overflow-y: auto`，放入 `.scroll-container` 后应改为 `overflow: visible`，由外层控制滚动
2. **`.input-area` 的 `margin-top: auto`**：在新布局的 flex column 中仍然有效，自动推到底部
3. **刻度导航定位**：`.tick-nav` 当前相对于 `.layout-center` 定位，新结构下需调整定位上下文
4. **欢迎状态**：`#app.welcome-state` 不再存在，欢迎状态的居中逻辑需要调整
5. **`#headerTitle`**：当前是 `.header-title` 内的 `span`，新结构下改为 `.main-title`，保留 ID
6. **品牌样式复用**：`.brand-logo` 复用 `.left-panel-logo`，`.brand-title` 复用 `.left-panel-title h1`，`.brand-subtitle` 复用 `.left-panel-title .subtitle`
