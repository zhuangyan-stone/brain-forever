# 自定义 Tooltip 组件方案

## 背景

Ubuntu + Edge 下浏览器原生 `title` 属性 tooltip 会触发整个窗口闪烁（甚至 GNOME Dash 栏都被闪出），这是 Chromium on Linux 的渲染 bug。Windows Edge 和 Firefox 均正常。

## 方案：自定义 Tooltip 组件

用 JS + CSS 实现一个轻量 tooltip 组件，完全替代浏览器原生 `title` 渲染，彻底绕过 Edge bug。

## 设计

### 1. 组件文件

| 文件 | 用途 |
|------|------|
| `frontend/static/components/tooltip.css` | Tooltip 样式 |
| `frontend/static/components/tooltip.js` | Tooltip 逻辑 |

### 2. 使用方式

```html
<!-- 方式一：data-tooltip 属性（推荐，最简洁） -->
<button data-tooltip="切换主题">...</button>

<!-- 方式二：JS 编程式调用 -->
import { showTooltip, hideTooltip } from './components/tooltip.js';
```

### 3. 行为

- `mouseenter` 延迟 ~300ms 后显示 tooltip
- `mouseleave` / `click` / `scroll` 立即隐藏
- tooltip 自动定位：优先显示在元素上方，空间不足则下方
- 跟随鼠标位置（水平居中于元素，垂直在元素上方/下方）
- 支持主题变量（暗色/亮色自动适配）
- 不影响 `pointer-events`（不干扰按钮点击）

### 4. CSS 设计

```css
.tooltip {
    position: fixed;
    z-index: 99999;
    padding: 6px 12px;
    border-radius: 6px;
    font-size: 0.8rem;
    line-height: 1.4;
    white-space: nowrap;
    pointer-events: none;
    opacity: 0;
    transition: opacity 0.15s ease;
    /* 主题色 */
    background: var(--tooltip-bg, #333);
    color: var(--tooltip-text, #fff);
    box-shadow: 0 2px 8px rgba(0,0,0,0.2);
}
.tooltip.visible {
    opacity: 1;
}
```

主题变量在 `theme.css` 中定义：
```css
:root, [data-theme="dark"] {
    --tooltip-bg: #2d2d2d;
    --tooltip-text: #e0e0e0;
}
[data-theme="light"] {
    --tooltip-bg: #616161;
    --tooltip-text: #ffffff;
}
```

### 5. JS 逻辑

```javascript
// tooltip.js
let tooltipEl = null;
let showTimer = null;
let currentTarget = null;

export function initTooltip() {
    // 使用事件委托监听所有 [data-tooltip] 元素
    document.addEventListener('mouseover', onMouseOver);
    document.addEventListener('mouseout', onMouseOut);
    document.addEventListener('click', hideTooltip);
    window.addEventListener('scroll', hideTooltip, true);
}

function onMouseOver(e) {
    const target = e.target.closest('[data-tooltip]');
    if (!target) return;
    if (target === currentTarget) return;
    
    currentTarget = target;
    clearTimeout(showTimer);
    showTimer = setTimeout(() => showTooltip(target), 300);
}

function onMouseOut(e) {
    const target = e.target.closest('[data-tooltip]');
    if (!target) return;
    
    clearTimeout(showTimer);
    hideTooltip();
    currentTarget = null;
}

function showTooltip(target) {
    hideTooltip(); // 移除旧 tooltip
    
    const text = target.getAttribute('data-tooltip');
    if (!text) return;
    
    tooltipEl = document.createElement('div');
    tooltipEl.className = 'tooltip';
    tooltipEl.textContent = text;
    document.body.appendChild(tooltipEl);
    
    // 定位
    positionTooltip(target);
    
    // 触发显示动画
    requestAnimationFrame(() => {
        tooltipEl.classList.add('visible');
    });
}

function positionTooltip(target) {
    const rect = target.getBoundingClientRect();
    const tipRect = tooltipEl.getBoundingClientRect();
    const gap = 6;
    
    // 默认在上方
    let top = rect.top - tipRect.height - gap;
    let left = rect.left + (rect.width - tipRect.width) / 2;
    
    // 上方空间不足，放到下方
    if (top < 4) {
        top = rect.bottom + gap;
    }
    
    // 水平不超出视口
    if (left < 4) left = 4;
    if (left + tipRect.width > window.innerWidth - 4) {
        left = window.innerWidth - tipRect.width - 4;
    }
    
    tooltipEl.style.top = top + 'px';
    tooltipEl.style.left = left + 'px';
}

export function hideTooltip() {
    if (tooltipEl) {
        tooltipEl.remove();
        tooltipEl = null;
    }
}
```

### 6. 替换计划

将所有 `title` 属性替换为 `data-tooltip` 属性，并移除 JS 中动态设置的 `.title`。

#### HTML 模板（index.html）

| 行 | 元素 | 原 title | 改为 |
|----|------|----------|------|
| 98 | newChatBtn | `开启新对话` | `data-tooltip="开启新对话"` |
| 106 | themeToggle | `切换主题` | `data-tooltip="切换主题"` |
| 146 | deepThinkBtn | `深度思考` | `data-tooltip="深度思考"` |
| 152 | webSearchBtn | `智能搜索` | `data-tooltip="智能搜索"` |
| 163 | attachBtn | `上传附件` | `data-tooltip="上传附件"` |
| 168 | sendBtn | `发送` | `data-tooltip="发送"` |

#### JS 动态设置

| 文件 | 行 | 原代码 | 改为 |
|------|----|--------|------|
| chat.js:109 | themeToggle.title | `切换到亮/暗色主题` | `themeToggle.dataset.tooltip = '...'` |
| chat.js:269 | globalToggleButton.title | `切换侧边栏` | `globalToggleButton.dataset.tooltip = '...'` |
| chat-reasoning.js:100 | toggleBtn.title | `展开/折叠思考内容` | `toggleBtn.dataset.tooltip = '...'` |
| chat-markdown.js:128 | btn.title | `复制代码块` | `btn.dataset.tooltip = '...'` |
| chat-ticknav.js:54 | topArrow.title | `向上翻动` | `topArrow.dataset.tooltip = '...'` |
| chat-ticknav.js:122 | bottomArrow.title | `向下翻动` | `bottomArrow.dataset.tooltip = '...'` |
| chat-ui.js:139 | info.title | token 估算提示 | `info.dataset.tooltip = '...'` |
| chat-ui.js:179,201 | groupDeleteBtn.title | `删除本组对话` | `groupDeleteBtn.dataset.tooltip = '...'` |
| chat-ui.js:243 | copyBtn.title | `复制当前消息内容` | `copyBtn.dataset.tooltip = '...'` |

#### 初始化

在 [`chat.js`](frontend/static/chat.js) 的初始化流程中调用 `initTooltip()`。

### 7. 关于标题历史下拉按钮

之前已注释掉 [`chat-ui.js`](frontend/static/chat-ui.js) 中 `createTitleDropdownBtn` 和 `updateTitleDropdownBtn` 的逻辑。如果未来要恢复，那个按钮的 `title` 也要改为 `data-tooltip`。

## 实施步骤

1. 创建 `frontend/static/components/tooltip.css` — tooltip 样式
2. 在 `theme.css` 中添加 `--tooltip-bg` 和 `--tooltip-text` 主题变量
3. 创建 `frontend/static/components/tooltip.js` — tooltip 逻辑
4. 在 `index.html` 中引入 `tooltip.css`
5. 在 `index.html` 中将所有 `title` 替换为 `data-tooltip`
6. 在各 JS 文件中将 `.title =` 替换为 `.dataset.tooltip =`
7. 在 `chat.js` 初始化中调用 `initTooltip()`
8. 测试验证
