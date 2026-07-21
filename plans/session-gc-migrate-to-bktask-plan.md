# Session GC 迁移到 bktask 实施计划

## 背景

当前 Session GC 使用独立的 goroutine + `time.Ticker` 驱动。为统一所有周期性后台任务的管理，将其迁移到 `bktask`（全局后台任务队列）。

同时，统一配置模式：增加 `enabled` 开关，TOML key 从 `[session-gc]` 改为 `[session-gc-task]`，与其他后台任务（`[trait-task]`、`[excerpt-task]`）保持一致。

## 涉及文件及变更

| # | 文件 | 操作 | 说明 |
|---|------|------|------|
| 1 | `internal/config/config.go` | 修改 | 改 key 为 `session-gc-task`，增加 `Enabled` 字段 |
| 2 | `internal/session/manager.go` | 修改 | 暴露 `GCOnce()`，移除 `StartGC()` 中的 goroutine/ticker |
| 3 | `internal/agent/init.go` | 修改 | 移除 `StartGC(ctx)` 调用 |
| 4 | `internal/tasks/session_gc_job.go` | **新建** | 注册函数 `RegisterPeriodicSessionGC` |
| 5 | `cmd/server/main.go` | 修改 | 在 bktask 块中添加 GC 注册 |
| 6 | `bin.template/settings_template/server.template.toml` | 修改 | 配置节改名为 `session-gc-task` 并增加注释 |
| 7 | `bin/settings/server.toml` | 修改 | 配置节改名为 `session-gc-task` |

---

## 步骤 1：修改配置定义

**文件：** `internal/config/config.go`

### 1.1 改 struct tag 并增加 Enabled 字段

```go
// 改前：
SessionGC   SessionGCConfig   `toml:"session-gc"`

// 改后：
SessionGC   SessionGCConfig   `toml:"session-gc-task"`
```

### 1.2 SessionGCConfig 增加 `Enabled` 字段

```go
type SessionGCConfig struct {
    // Enabled enables the periodic session GC task. Default: true.
    Enabled bool `toml:"enabled"`

    // AnonymousTTLMinutes ...
    AnonymousTTLMinutes int `toml:"anonymous_ttl_minutes"`
    // LoggedInTTLMinutes ...
    LoggedInTTLMinutes int `toml:"logged_in_ttl_minutes"`
    // IntervalMinutes ...
    IntervalMinutes int `toml:"interval_minutes"`
}
```

### 1.3 DefaultConfig 中设置默认值

```go
SessionGC: SessionGCConfig{
    Enabled:              true,
    AnonymousTTLMinutes:  60,
    LoggedInTTLMinutes:   1440,
    IntervalMinutes:      10,
},
```

---

## 步骤 2：暴露 GCOnce 方法

**文件：** `internal/session/manager.go`

### 2.1 新增 `GCOnce()` 公共方法

```go
// GCOnce performs one sweep of expired sessions.
// Exported for use as a periodic task in the background task queue.
func (m *Manager) GCOnce() {
    m.gc()
}
```

### 2.2 清空 `StartGC()` — 移除 goroutine + ticker

```go
// StartGC starts the background session GC goroutine.
// Deprecated: Use GCOnce with the background task queue instead.
// Now a no-op kept for backward compatibility during migration.
func (m *Manager) StartGC(ctx context.Context) {}
```

### 2.3 简化 `Manager` 结构体

移除 `gcStop` 字段：

```go
type Manager struct {
    Mu       sync.RWMutex
    Sessions map[string]*Session
    redis    *cache.RedisSessionStore
    Ctx      context.Context
    logger   zylog.Logger
    gcConfig GCConfig
    // gcStop   context.CancelFunc  // REMOVED
}
```

### 2.4 简化 `Close()`

```go
func (m *Manager) Close() {
    m.Mu.Lock()
    defer m.Mu.Unlock()
    m.Sessions = make(map[string]*Session)
}
```

---

## 步骤 3：新建 session_gc_job.go

**文件：** `internal/tasks/session_gc_job.go`

遵循与其他任务一致的注册模式：

```go
package tasks

import (
    "time"
    "BrainForever/infra/zylog"
    "BrainForever/internal/session"
)

// RegisterPeriodicSessionGC registers the session GC as a recurring task
// in the global bktask queue. Must be called after InitGlobal().
func RegisterPeriodicSessionGC(
    interval time.Duration,
    sessionManager *session.Manager,
    logger zylog.Logger,
) {
    err := TheBkTaskQueue().AddRecurring(
        "session-gc",
        interval,
        func() error {
            sessionManager.GCOnce()
            return nil
        },
    )
    if err != nil {
        logger.Errorf("failed to register session GC task. %v", err)
        return
    }
    logger.Infof("session GC task registered (interval=%v)", interval)
}
```

---

## 步骤 4：修改 InitAgent

**文件：** `internal/agent/init.go`

移除 StartGC 调用：

```go
// REMOVED:
// chatHandler.GetSessionManager().StartGC(ctx)
```

---

## 步骤 5：修改 main.go

**文件：** `cmd/server/main.go`

在 bktask 块中，特征提取和语录提取注册之后，添加 GC 注册：

```go
// Register periodic session GC task.
if cfg.SessionGC.Enabled {
    gcInterval := time.Duration(cfg.SessionGC.IntervalMinutes) * time.Minute
    tasks.RegisterPeriodicSessionGC(
        gcInterval,
        chatHandler.GetSessionManager(),
        theLogger,
    )
}
```

---

## 步骤 6：更新模板配置文件

**文件：** `bin.template/settings_template/server.template.toml`

将 `[session-gc]` 改为 `[session-gc-task]`，增加 `enabled` 说明：

```toml
# ============================================================
# 周期性 Session GC 任务（可选）
# 控制内存中 session 对象的自动清理，防止长时间运行后内存泄漏。
# 注册到 bktask 后台任务队列，与 trait-task、excerpt-task 共享调度。
# 如果未配置此节，使用 DefaultConfig() 中的内置默认值。
# ============================================================
[session-gc-task]
# 是否启用
enabled = true
# 匿名 session 无活动超过此时间（分钟）则从内存中清除
anonymous_ttl_minutes = 60
# 已登录 session 无活动超过此时间（分钟）则从内存中清除
logged_in_ttl_minutes = 1440
# GC 扫描间隔（分钟）
interval_minutes = 10
```

---

## 步骤 7：更新运行时配置文件

**文件：** `bin/settings/server.toml`

同理将 `[session-gc]` → `[session-gc-task]` 并增加 `enabled`：

```toml
[session-gc-task]
enabled = true
anonymous_ttl_minutes = 10
logged_in_ttl_minutes = 20
interval_minutes = 5
```

---

## 影响范围与风险

| 项目 | 评估 |
|------|------|
| **配置兼容性** | ⚠️ 破坏性变更 — TOML key 从 `session-gc` 改为 `session-gc-task`。新旧配置不兼容，需手动更新配置文件和模板 |
| **功能影响** | 低 — `gc()` 核心逻辑完全不变 |
| **并发安全** | 不变 — `gc()` 内部已有 `RLock/Lock` 保护 |
| **重启行为** | 不变 — `bktask.Stop()` 在 defer 中执行 |
| **降级路径** | `enabled = false` 则不注册 GC 任务（不影响服务，session 仅内存占用增加） |

---

## 实施顺序

1. `internal/config/config.go` — 改 struct tag + 增加 `Enabled` 字段 + 更新 defaults
2. `internal/session/manager.go` — 暴露 `GCOnce()`，清空 `StartGC()`，简化 `Close()`
3. `internal/tasks/session_gc_job.go` — 新建注册函数
4. `internal/agent/init.go` — 移除 `StartGC(ctx)` 调用
5. `cmd/server/main.go` — 添加 GC 注册（带 `Enabled` 判断）
6. `bin.template/settings_template/server.template.toml` — 更新配置节
7. `bin/settings/server.toml` — 更新配置节
8. `go build ./cmd/server/` 验证编译通过
