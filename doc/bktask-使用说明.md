# bktask — 后台慢任务队列使用说明

## 概述

[`bktask`](../infra/bktask/bktask.go) 是一个**后台慢任务队列**，是本项目的基础设施之一。它面向那些不需要精确到秒级触发、但需要定期反复执行的后台作业——例如清理过期数据、刷新缓存、同步外部资源等。

### 设计定位

| 特性 | 说明 |
|------|------|
| **慢任务** | 检查轮次间隔以分钟/秒为单位（典型值 10 分钟），不适合高频任务 |
| **定时驱动** | 使用 `time.Ticker` 定期触发检查，而非主动唤醒或事件驱动 |
| **通用性** | 任务用纯函数表达 —— `func() error`，不限具体业务 |
| **自动包装** | 队列内部为每个 job 包装固定日志 + 自动重入队逻辑，用户只需 `Add()` 一次 |
| **并发安全** | 所有公开方法可在任意 goroutine 中安全调用 |

---

## 核心概念

### 1. 任务（[`BkgndTask`](../infra/bktask/bktask.go:30)）

```go
type BkgndTask struct {
    Job      func() error  // 要执行的函数
    OneShot  bool          // true=一次性, false=定期
    Interval time.Duration // 延迟/间隔时间
}
```

### 2. 调度方式

| `OneShot` | `Interval` | 行为 |
|---|---|---|
| `false` | 任意 | **定期任务**。每次执行完后自动重新 `Add()`，`Interval` 为两次执行之间的间隔 |
| `true` | `> 0` | 一次性延迟任务。等待 `Interval` 后执行一次，然后移除 |
| `true` | `== 0` | 一次性立即任务。下一轮检查到达时执行一次，然后移除 |

### 3. 自动包装机制

队列内部在 [`safeRun`](../infra/bktask/bktask.go:294) 中为每个 job 自动包裹三层逻辑：

```
① 固定日志：  "executing task (oneShot=false, interval=30m)"
② 执行 job：   user.Job()
③ 固定日志：  "task completed" 或 "job failed"
④ 自动重入队：若 OneShot==false → 自动 Add() 下一轮
```

用户看到的效果始终是：**调用一次 `Add()`，定期任务自动永远执行下去**。

### 4. 检查轮次间隔

由 [`New(checkInterval, logger)`](../infra/bktask/bktask.go:72) 的第一个参数指定，是整个队列的"心跳"频率：

```go
q := bktask.New(10*time.Minute, logger) // 每 10 分钟检查一次
q := bktask.New(30*time.Second, logger) // 每 30 秒检查一次
```

默认值为 10 分钟（如果传入 `<= 0` 的值）。

---

## API 速查

| 函数/方法 | 签名 | 说明 |
|-----------|------|------|
| [`New`](../infra/bktask/bktask.go:72) | `New(interval time.Duration, logger zylog.Logger) *TaskQueue` | 创建队列 |
| [`BkgndTask`](../infra/bktask/bktask.go:30) | `struct{ Job func() error; OneShot bool; Interval time.Duration }` | 任务定义 |
| [`Add`](../infra/bktask/bktask.go:100) | `Add(task BkgndTask) error` | 添加任务 |
| [`Start`](../infra/bktask/bktask.go:171) | `Start()` | 启动循环 |
| [`Stop`](../infra/bktask/bktask.go:192) | `Stop()` | 停止 + 清空 |
| [`Pause`](../infra/bktask/bktask.go:209) | `Pause()` | 暂停执行 |
| [`Resume`](../infra/bktask/bktask.go:220) | `Resume()` | 恢复执行 |
| [`Clear`](../infra/bktask/bktask.go:140) | `Clear()` | 清空任务列表 |

---

## 生命周期

```
        New()
          │
          ▼
    ┌───────────┐
    │   created  │
    └─────┬─────┘
          │ Start()
          ▼
    ┌───────────┐   Pause()   ┌───────────┐
    │  running  │────────────>│  paused   │
    └─────┬─────┘<────────────└───────────┘
          │          Resume()
          │ Stop()
          ▼
    ┌───────────┐
    │  stopped  │  (无法再 Start)
    └───────────┘
```

---

## 使用示例

### 定期任务

```go
q.Add(bktask.BkgndTask{
    Job:     cleanupTempFiles,
    OneShot: false,
    Interval: 30 * time.Minute, // 每 30 分钟执行一次
})
```

### 一次性任务（下一 tick 执行）

```go
q.Add(bktask.BkgndTask{
    Job:     initializeSomething,
    OneShot: true,
    // Interval 默认为 0 → 下一 tick 执行
})
```

### 一次性任务（延迟执行）

```go
q.Add(bktask.BkgndTask{
    Job:      sendDelayedReport,
    OneShot:  true,
    Interval: 1 * time.Hour, // 1 小时后执行一次
})
```

### 完整示例

```go
package main

import (
    "time"
    "BrainForever/infra/bktask"
    "BrainForever/infra/zylog"
)

func main() {
    logger, _ := zylog.NewLogger(zylog.Config{
        Name:    "bktask-demo",
        Console: zylog.ConsoleModeColor,
    })

    q := bktask.New(10*time.Minute, logger)
    q.Start()
    defer q.Stop()

    // 定期任务：每 30 分钟清理临时文件
    q.Add(bktask.BkgndTask{
        Job:      cleanupTempFiles,
        OneShot:  false,
        Interval: 30 * time.Minute,
    })

    // 一次性：立即执行
    q.Add(bktask.BkgndTask{
        Job:     initCache,
        OneShot: true,
    })

    select {}
}

func cleanupTempFiles() error { /* ... */ return nil }
func initCache() error        { /* ... */ return nil }
```

### Add 与 Start 的先后顺序

`Add()` 可以在 `Start()` **之前或之后**调用，效果相同：

```go
q := bktask.New(10*time.Minute, logger)

// 先 Add 再 Start
q.Add(taskA)
q.Start()

// 等价于先 Start 再 Add
q.Start()
q.Add(taskA)
```

---

## 工作原理

### 循环结构

```text
Start() → 创建 time.Ticker（每 I 时间触发一次）
              │
              ▼  ticker.C
         ┌─────────────┐
         │  loop()      │ goroutine
         │  select {    │
         │   stopCh → 退出
         │   ticker.C → checkAndRun()
         │  }
         └─────────────┘
```

### 一次检查的过程（[`checkAndRun`](../infra/bktask/bktask.go:253)）

1. **加锁** → 遍历所有任务，`nextRun <= now` 的到期，移出队列 → **解锁**
2. 每个到期 job 通过 `go safeRun()` **异步执行**

### safeRun 包装层

```go
func (q *TaskQueue) safeRun(entry *taskEntry) {
    defer recover() ...                    // panic 保护
    log "executing task ..."               // ① 固定日志
    err := entry.task.Job()                // ② 执行用户的 job
    if err != nil { log "job failed ..." } // ③ 固定日志
    else           { log "task completed" }
    if !entry.task.OneShot {               // ④ 定期任务自动重入队
        q.Add(BkgndTask{Job, OneShot: false, Interval})
    }
}
```

---

## 实现参考

- 源代码：[`infra/bktask/bktask.go`](../infra/bktask/bktask.go)
- 测试：[`infra/bktask/bktask_test.go`](../infra/bktask/bktask_test.go)
- 日志接口：[`infra/zylog/logger.go`](../infra/zylog/logger.go)
- 编码规范：[`coding-standards/golang源代码规范.md`](../coding-standards/golang源代码规范.md)、[`coding-standards/出错信息格式规范.md`](../coding-standards/出错信息格式规范.md)
