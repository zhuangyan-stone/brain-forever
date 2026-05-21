# Scroll Anchoring 假阳性 — 最终修复方案

## 根因

当前 scroll handler 有 200ms 节流。时序如下：

```mermaid
sequenceDiagram
    participant User as 用户操作
    participant Prepare as prepareChat
    participant Auto as autoScrollToBottom
    participant Throttle as 节流锁 200ms
    participant Handler as Scroll Handler
    participant Collapse as collapseInputArea
    participant Anchor as Scroll Anchoring

    Note over User,Anchor: 用户发送消息前可能已触发非流式 handler
    User->>Handler: 之前滚动 → 非流式 handler
    Handler->>Collapse: 刻度变化 → 折叠输入面板
    Collapse->>Anchor: 布局变化
    Anchor->>Anchor: scrollTop 被动偏移 104px
    Note over Prepare,Anchor: scrollThrottleTimer=null 在 handler 开头 → 新定时器启动

    Prepare->>Auto: 流式开始，强制滚动到底部
    Auto->>Auto: scrollTop=33855 (已到底) ✅
    Note over Auto,Throttle: 但此 scroll 事件被节流锁拦截

    Note over Throttle,Handler: 200ms 后，上一步的非流式 handler 的新定时器触发
    Handler->>Handler: isStreaming=true (流式已经开始)
    Handler->>Handler: isScrolledToBottom → false ❌ (scrollTop已被scroll anchoring偏移)
    Handler->>Handler: userScrolledUp=true ❌
```

**真实原因**：streaming 分支没有主动同步 scrollTop，而是被动检查 `isScrolledToBottom()`。而 scrollTop 可能已被之前的 scroll anchoring 偏移。

## 修复

**streaming 分支中，如果 `userScrolledUp=false`（用户未要求停止自动滚动），直接 `scrollTop = scrollHeight`，覆盖所有 scroll anchoring 偏移。**

```javascript
// 改动后
if (state.isStreaming) {
    collapseInputArea();

    if (!state.userScrolledUp) {
        // ✅ 强制滚动到底部，覆盖 scroll anchoring 造成的任何偏移
        dom.scrollContainer.scrollTop = dom.scrollContainer.scrollHeight;
    } else if (isScrolledToBottom()) {
        // 用户滚回底部，恢复自动滚动
        state.userScrolledUp = false;
    }

    scrollThrottleTimer = null;
    return;
}
```

这样改动后：
1. `collapseInputArea()` 仍然在 streaming 分支中调用 ✅
2. layout 变化后的 scroll anchoring 偏移被 `scrollTop=scrollHeight` 直接覆盖 ✅
3. `userScrolledUp=true` 时停止强制滚动，用户可自由阅读 ✅
4. `userScrolledUp` 不在此分支中设置（不再有假阳性） ✅

## 改动文件

只改 [`chat.js`](frontend/static/chat.js) streaming 分支。

## 验证

| 场景 | 预期 |
|------|------|
| 流式开始，scroll anchoring 偏移了 scrollTop | ✅ `scrollTop=scrollHeight` 覆盖，保持在底部 |
| 用户滚轮上滚（throttleRender 的 autoScrollToBottom 仍会尝试滚动） | ⚠️ 需在别处设 userScrolledUp=true |
| 用户滚回底部 | ✅ `isScrolledToBottom()` → `userScrolledUp=false` |
