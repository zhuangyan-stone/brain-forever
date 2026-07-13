# 侧边栏 Active Chat 多实例高亮问题 — 方案讨论

## 问题描述

在 **分类 tab** 下，同一个 chat 可能出现在：
1. **收藏栏**（`favoritesGroups`）— 被用户收藏，放入一个或多个自定义目录
2. **智能分类**（`chatGroups`）— 被 AI 打上多个标签（如"技术"+"学习"）

当一个 chat 出现在多个位置时，点击选中它，设置 `activeChatSN = sn`，Alpine 模板对所有 `.chat-item` 用以下条件渲染：

```html
:class="{ active: chat.sn === $store.chats.activeChatSN }"
```

于是该 chat **所有出现的位置** 同时获得 `.active` 样式（蓝色左边框 + accent 色标题），出现"两个 active item"的情况。

## 数据模型

- [`chat_tags`](internal/local/store/scheme.go:61) — 一个 chat 可以有多条 tag 记录（M:N 关系）
- [`chat_favorites`](internal/local/store/scheme.go:74) — 一个 chat 可以有多条收藏记录（按 custom_tag 分组，唯一约束 `(chat_sn, custom_tag)`）
- [前端 `favoritesGroups`](frontend/static/alpine-store.js:317) — `{customTag: [chat, ...]}`，按用户自定义目录分组
- [前端 `chatGroups`](frontend/static/alpine-store.js:316) — `{tagName: [chat, ...]}`，按 AI 标签分组

## 渲染路径

1. [时间线 tab](frontend/index.html:180) — `x-for="chat in group.items"`，只出现一次
2. [收藏栏](frontend/index.html:328) — `x-for="chat in items"`，按 custom_tag 多次出现
3. [智能分类](frontend/index.html:373) — `x-for="chat in items"`，按 tag 多次出现

所有渲染点共用 `activeChatSN` 作为唯一判定条件。

## 候选方案

### 方案 A：上下文感知（推荐 ✅）

引入 `activeChatSource` 记录最后一次点击来自哪个区间，仅在对应区间显示完整 `.active` 样式。

**新增 store 字段：**
- `activeChatSource: 'timeline' | 'favorites' | 'category' | null`

**点击时设置（修改 `selectChat`）：**
```js
selectChat(sn, source) {
    this.activeChatSource = source;
    // ...
}
```

**分类区间内增设 `activeSubSource`：**
记录具体从哪个 tag group 点击的，避免同一分类区间的多个 tag group 重复高亮。

**模板修改：**
```html
<!-- 收藏栏 -->
:class="{ 
    active: chat.sn === $store.chats.activeChatSN 
        && $store.chats.activeChatSource === 'favorites' 
        && $store.chats.activeSubSource === customTag 
}"

<!-- 智能分类 -->
:class="{ 
    active: chat.sn === $store.chats.activeChatSN 
        && $store.chats.activeChatSource === 'category'
        && $store.chats.activeSubSource === tag 
}"
```

**优势：** 精确，用户总能知道自己"从哪点进来的"
**劣势：** 切换 tab 或 group 折叠/展开后，active 可能消失直到再次点击
**缓解：** 切换到新 tab 时，如果该 tab 包含 active chat，自动更新 source

### 方案 B：首次匹配优先

在每个渲染周期中，同一个 SN 只对第一个 `.chat-item` 应用 `.active`，后续出现的不再应用。

**实现思路**——store 中加一个判断方法：
```js
_renderActiveSet: null,

isPrimaryActive(sn) {
    if (sn !== this.activeChatSN) return false;
    // 不在 set 中 → 首次匹配 → 标记后返回 true
    // 已在 set 中 → 非首次 → 返回 false
}
```

每次 `activeChatSN` 变化时重置 `_renderActiveSet`。

**优势：** 改动极小，保留"当前对话"的感知
**劣势：** 依赖于 Alpine 的 x-for 渲染顺序（收藏栏 → 智能分类），顺序不确定时结果不稳定

### 方案 C：区间主次样式区分

所有出现位置都显示"活跃"状态，但**主区间**（点击来源）用完整 `.active` 样式，**次区间**用较淡的指示样式（如只有背景色，无左边框）。

```css
.chat-item.active-sub {
    background: var(--bg-user);
    /* 无左边框，无 accent 色标题 */
    opacity: 0.8;
}
```

**优势：** 用户在所有位置都能感知"这个 chat 是当前的"，但视觉层级清晰
**劣势：** 需要额外 CSS + 模板条件判断，复杂度略高

### 方案 D：后台去重

在后端返回分组数据时，确保每个 chat 只出现在一个分组中（如只出现在第一个匹配的 tag 下）。或在 `loadChatGroups` / `loadFavorites` 后前端去重。

**优势：** 渲染逻辑最简单
**劣势：** 丢失信息——用户无法在多处看到同一个 chat，反直觉

### 方案 E：保持现状 + 视觉优化

接受多 active 的现实，但优化视觉效果：多个 active 的 chat-item，其左侧边框采用渐变或半透明，让用户视觉上知道"这是同一个对话的延伸引用"。

**优势：** 改动最小
**劣势：** 只是缓解不是解决，用户仍可能困惑

---

## 推荐

**方案 A（上下文感知）+ 方案 B 的混合体**：
1. 追踪点击来源区间 (`activeChatSource`)
2. 在同一区间内（如智能分类），如果有多个 tag group 包含该 chat，只在第一个匹配的 group 显示 `.active`（用方案 B 的首次匹配）
3. 不同区间不做跨区高亮（收藏栏的 chat 不会因为"智能分类"中有它就高亮）

这样保证**最多只有一个 chat-item 显示 `.active` 样式**，清晰无歧义。

---

## 需要讨论的问题

1. **你倾向于哪个方案？** 还是有其他思路？
2. **跨 tab 行为**：在分类 tab 选了 chat，切回时间线 tab，时间线里是否应该高亮？
3. **收藏 vs 智能分类**：如果一个 chat 同时出现在收藏栏和智能分类中，点击收藏栏的条目，智能分类那边是否要显示任何指示？
