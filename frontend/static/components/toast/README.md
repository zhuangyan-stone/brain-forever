# Toast 组件

## 文件结构

```
components/toast/
├── toast.js       ← 参考实现（与 HTML 内联逻辑一致）
├── toast.css      ← Toast 样式 + Alpine x-transition 动画类
└── README.md      ← 本文件
```

## 架构说明

### 为什么 x-data 是内联对象而非函数引用？

**时序约束**：

```
HTML 解析
  → <script defer src="alpine.min.js"> 执行
    → Alpine 初始化，扫描 DOM 中的 x-data/*-show 等指令
    → 此时 toastManager 函数尚未注册！（ES Module 尚未执行）
  → <script type="module" src="chat.js"> 执行
    → ES Module 的导出函数才可用
```

因为 Alpine 的 `defer` 脚本比 ES Module 先执行，所以不能使用 `x-data="toastManager()"` 这种函数引用方式——当 Alpine 初始化时，`toastManager` 还不在 Alpine 的组件注册表中。

**解决方案**：在 HTML 中使用内联对象字面量：

```html
<div x-data="{
    toasts: [],
    _nextId: 0,
    addToast(detail) { ... }
}"
```

Alpine 直接解析对象字面量，无需函数名查找。

### 后续改进方向

如果后续需要支持 `x-data="functionName()"` 模式，有以下方案：

1. **Alpine.data() 注册**：在 `alpine-bridge.js` 中调用 `Alpine.data('toastManager', toastManager)`，但前提是该脚本在 Alpine 初始化前执行
2. **非模块脚本**：使用 `<script>`（非 module）加载组件定义，确保在 Alpine.js 之前执行
3. **魔术方法**：用 `Alpine.magic()` 注册全局方法

## 使用方式

```javascript
// 任意 JS 文件中触发 Toast
window.dispatchEvent(new CustomEvent('toast-show', {
    detail: {
        message: '操作成功',
        type: 'success',   // 'error' | 'success' | 'info'
        duration: 4000     // 可选，默认 4000ms
    }
}));
```

`showToast()` 函数（定义在 `chat-ui.js`）已封装此逻辑，可直接调用：

```javascript
showToast('操作成功', 'success', 4000);
```
