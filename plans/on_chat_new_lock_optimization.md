# OnNewChat 锁优化方案

## 问题分析

[`OnNewChat`](internal/agent/on_chat_new.go:22) 当前的分段加锁模式：

```go
// 第1段：session.mu
session.mu.Lock()
ensureDBSession(session)
dbChatID := session.currentChat.dbChat.ID
title := session.currentChat.title
titleState := int(session.currentChat.titleState)
session.mu.Unlock()

// 第2段：chatsMu（遍历查找 SN）
var sn string
if dbChatID > 0 {
    session.chatsMu.Lock()
    for _, c := range session.chats {
        if c.ID == dbChatID {
            sn = c.SN
            break
        }
    }
    session.chatsMu.Unlock()
}
```

### 两个问题

1. **不必要的 `chatsMu` 遍历**：[`store.Chat`](internal/store/chats.go:21) 结构体本身就包含 `SN` 字段。`ensureDBSession` 执行后，`session.currentChat.dbChat` 已经指向了包含 `SN` 的 `*store.Chat`。完全不需要再锁 `chatsMu` 去遍历 `session.chats` 列表查找 SN。

2. **锁嵌套依赖**：`ensureDBSession` 内部调用 [`addChatToList`](internal/agent/types.go:370)，后者会锁 `chatsMu`。这是在持有 `session.mu` 的情况下再获取 `chatsMu`。虽然当前没有反向锁顺序（先 `chatsMu` 再 `session.mu`），但这是一个隐性的锁顺序依赖，增加了未来重构时的死锁风险。

## 优化方案

将整个函数简化为**单次 `session.mu` 加锁**，直接从 `dbChat.SN` 读取，完全消除 `chatsMu` 的加锁：

```go
func (h *ChatAgent) OnNewChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// 单次加锁：确保 DB session 存在，并读取所有需要的字段
	session.mu.Lock()
	ensureDBSession(session)

	dbChat := session.currentChat.dbChat
	sn := ""
	if dbChat != nil {
		sn = dbChat.SN
	}
	title := session.currentChat.title
	titleState := int(session.currentChat.titleState)
	session.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sn":          sn,
		"title":       title,
		"title_state": titleState,
	})
}
```

### 变更说明

| 变更项 | 原代码 | 新代码 |
|--------|--------|--------|
| SN 获取方式 | 锁 `chatsMu` + O(n) 遍历 `session.chats` | 直接从 `dbChat.SN` 读取（O(1)） |
| 锁次数 | 2 次（`session.mu` + `chatsMu`） | 1 次（仅 `session.mu`） |
| 匿名用户处理 | `dbChatID > 0` 判断后决定是否查 SN | `dbChat != nil` 判空后直接读 `SN` |
| 锁嵌套 | `session.mu` → `chatsMu`（`ensureDBSession` → `addChatToList`） | 同上（未改变 `ensureDBSession` 内部逻辑） |

### 安全性分析

- **竞态安全**：`dbChat` 指针在 `session.mu` 保护下读取，`dbChat.SN` 是创建后只读的字段，不会被其他 goroutine 修改。
- **行为等价**：
  - 登录用户：`ensureDBSession` 创建/确认 DB 记录，`dbChat.SN` 为生成的 SN（如 `chat-<uuid>`），与原来从 `session.chats` 遍历得到的值一致。
  - 匿名用户：`ensureDBSession` 同样会在 `anonymous.db` 中创建记录，`dbChat.SN` 同样有值。原代码中 `dbChatID > 0` 为 true 时才会查 SN，新代码中 `dbChat != nil` 也为 true，行为一致。
  - 极端情况（`InsertChat` 失败）：`ensureDBSession` 中 `dbChat` 保持原样（nil 或 ID=0），新代码中 `sn` 为空字符串，与原代码中 `dbChatID > 0` 为 false 时 `sn` 为空字符串一致。

## 执行步骤

1. 修改 [`internal/agent/on_chat_new.go`](internal/agent/on_chat_new.go) 第 31-51 行，应用上述优化。
2. 编译验证：`go build ./...`
3. 运行测试：`go test ./internal/agent/...`
