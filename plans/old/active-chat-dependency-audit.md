# chats.active 依赖审计

## ✅ 安全类别：用户主动交互（操作的是当前活跃页面）

| 文件 | 行 | 用途 | 说明 |
|------|-----|------|------|
| `chat.js:88` | `activeChat` | AI标题按钮点击 → `fetchChatTitle` | 用户主动点击，操作当前活跃对话 |
| `chat.js:691` | `activeChat.sn` | 停止按钮 → abort stream | 用户主动停止当前对话的流 |
| `chat.js:753` | `chats.active` | header标题点击编辑 | 用户主动编辑当前标题 |
| `chat-copy.js:273` | `chats.active.groups` | 复制按钮 | 用户复制当前对话内容 |
| `msg-delete-dialog.js:34` | `chats.active.groups` | 删除消息 | 用户删除当前对话中的消息 |
| `buttons.js:60` | `$store.chats.active?.isStreaming` | Alpine模板 | 响应式绑定当前对话的流式状态 |
| `chat-container.js:41` | `chats.active.groups` | Alpine组件 | 渲染当前对话的消息组 |
| `chat-ui.js:221` | `chats.active.userScrolledUp` | 自动滚动 | 当前对话的滚动状态 |

## ✅ 安全类别：同步初始化阶段（用户尚未切换）

| 文件 | 行 | 用途 | 说明 |
|------|-----|------|------|
| `chat-sse.js:69` | `chats.active` | `addUserMessage` | sendMessage同步阶段，用户尚未切换 |
| `chat-sse.js:102` | `activeChat.isStreaming` | `prepareChat` | 防重复发送检查 |
| `chat-sse.js:117` | `chats.active.userScrolledUp` | `prepareChat` | 重置滚动状态 |
| `chat-sse.js:128-130` | `chats.active.sn` | `prepareChat` → promoteBlankItem | 获取临时SN，同步阶段 |

## ✅ 安全类别：对比检查（仅用于比较，不修改）

| 文件 | 行 | 用途 | 说明 |
|------|-----|------|------|
| `chat-sse-responser.js:116` | `chats.active.sn === this.stream.sn` | `isActive` 判断 | 仅检查，不修改 |
| `chat-sse.js:446` | `chats.active.sn === streamSN` | `isActiveStream` 判断 | 仅检查，不修改 |

## ✅ 安全类别：已在 switchTo 之后（active 已指向正确对象）

| 文件 | 行 | 用途 | 说明 |
|------|-----|------|------|
| `chat-list.js:188-191` | `chats.active.groups = []` | `selectChat` 中 switchTo 之后 | ✓ |
| `chat-list.js:234-236` | `chats.active.titleState` | `selectChat` 中 switchTo 之后 | ✓ |

## ✅ 安全类别：仅在活跃分支中执行（isActive == true）

| 文件 | 行 | 用途 | 说明 |
|------|-----|------|------|
| `chat-sse-responser.js:476-478` | `chats.active.groups` | `_applyDoneToDOM` 中 | 位于 `isActive` 分支内 ✓ |
| `chat-sse-responser.js:489-491` | `chats.active.groups` | `_applyDoneToDOM` 中 | 位于 `isActive` 分支内 ✓ |

## ⚠️ 已修复类别：流式完成后可能指向错误对话

| 文件 | 修复 | 状态 |
|------|------|------|
| `chat-api.js:73` | `fetchChatTitle` titleState 检查改为 `getOrCreate(sn)` | ✅ 已修复 |
| `chat-api.js:91` | `fetchChatTitle` 脏对话检查改为 `getOrCreate(sn)` | ✅ 已修复 |
| `chat-api.js:116` | `fetchChatTitle` 异步回调中的 titleState 检查改为 `getOrCreate(targetSN)` | ✅ 已修复 |
| `chat-sse.js:392` | `applyAIAutoTitle` 改为 `getOrCreate(sn)` | ✅ 已修复 |
| `chat-sse.js:490` | `syncSidebarChatEntry` 不再调用 `/api/chat/current` | ✅ 已修复 |
| `chat-sse-responser.js:258` | `onChatCreated` 保留 `activeChatSN` | ✅ 已修复 |

## ❌ 待修复：selectChat 中 header 标题 fallback

`chat-list.js:228-231`：当 `switchChat` 返回空标题时，header 残留上一个对话的标题。

```javascript
// 当前：只信任后端返回的标题
if (result.title) {
    updateHeaderTitle(result.title);
}

// 修复：后端空标题时 fallback 到 Alpine store
if (result.title) {
    updateHeaderTitle(result.title);
} else {
    // switchTo 已执行，chats.active 指向目标对话
    try {
        var hc = window.Alpine.store('chats');
        if (hc && hc.active && hc.active.title) {
            updateHeaderTitle(hc.active.title);
        }
    } catch(e) {}
}
```
