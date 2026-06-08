# BrainForever 双可执行文件方案 v2（最终版）

## 方案 A：单 Module + internal/local + internal/remote

保持单一 [`go.mod`](go.mod)，将 `internal/` 做子域划分：

```
brain-forever/                      ← module BrainForever
├── cmd/
│   ├── local-server/
│   │   ├── main.go                 ─ local-server 入口（从当前 main.go 提取）
│   │   └── local-server.toml       ─ 本地端配置文件
│   └── remote-server/
│       ├── main.go                 ─ remote-server 入口（Hello-World 级 stub）
│       └── remote-server.toml      ─ 远程端配置文件
├── internal/
│   ├── local/                      ← 现有 internal/ 整体移入
│   │   ├── agent/                  ─ Chat agent 逻辑
│   │   ├── config/                 ─ 配置结构体
│   │   ├── logger/                 ─ 日志
│   │   └── store/                  ─ SQLite 数据库操作
│   └── remote/                     ← 新增：remote-server 私有代码
│       └── config/                 ─ 远程端配置结构体
├── infra/                          ← 共享（不变）
│   ├── httpx/
│   ├── llm/
│   ├── embedder/
│   ├── searcher/
│   ├── i18n/                       ← local-server 使用（remote-server 暂不处理）
│   └── zylog/
├── frontend/                       ← 仅 local-server 服务
├── lang/                           ← local-server 使用（remote-server 暂不处理）
├── toolset/                        ← SN 生成等工具函数
├── data/                           ← local-server 运行时数据（SQLite 文件）
├── deploy/                         ← 部署示例配置文件
├── go.mod                          ← 不变
├── go.sum
├── b.sh / b.bat                    ← 构建两个 binary
└── brain-forever.ps1              ← 启动 local-server
```

---

## 代码归属矩阵（最终确认）

| 包 | 路径 | 归属 | 说明 |
|---|------|------|------|
| **agent** | `internal/local/agent/` | ✅ local-server | Chat 核心逻辑 |
| **config** | `internal/local/config/` | ✅ local-server | 本地端配置 |
| **logger** | `internal/local/logger/` | ✅ local-server | 本地日志 |
| **store** | `internal/local/store/` | ✅ local-server | SQLite 数据库 |
| **httpx** | `infra/httpx/` | 🔄 共享 | HTTP 工具、CORS、SSE |
| **llm** | `infra/llm/` | 🔄 共享 | LLM API 客户端 |
| **embedder** | `infra/embedder/` | 🔄 共享 | 文本嵌入 |
| **searcher** | `infra/searcher/` | 🔄 共享 | 联网搜索 |
| **i18n** | `infra/i18n/` | ✅ local-server | 国际化工具 |
| **zylog** | `infra/zylog/` | 🔄 共享 | 日志库 |
| **lang** | `lang/` | ✅ local-server | 多语言文件 |
| **remote config** | `internal/remote/config/` | ✅ remote-server | 远程端配置 |
| **frontend** | `frontend/` | ✅ local-server | 静态文件 |
| **data** | `data/` | ✅ local-server | 运行时数据 |

---

## 配置文件设计（TOML 格式）

项目已依赖 `github.com/BurntSushi/toml v1.6.0`，i18n 文件也是 `.toml`，配置格式自然选择 TOML。

### local-server.toml

```toml
[server]
addr = ":8080"
remote_url = "http://localhost:9090"

[logger]
file = "log/local-server.log"
level = "TRACE"

[data]
dir = "./data"

[frontend]
dir = "./frontend"
cache_disable = false
```

### remote-server.toml（最小化 stub）

```toml
[server]
addr = ":9090"

[logger]
file = "log/remote-server.log"
level = "INFO"
```

### 加载优先级

```
--config <path> CLI 参数           最高
  → CONFIG_PATH 环境变量
    → ./cmd/<binary>/<binary>.toml  最低（开发默认）
```

---

## 运行时数据目录

`./data/` 相对路径维持不变，通 `config.data.dir` 可配置基路径。

local-server 部署结构：
```
/opt/brain-forever/
├── local-server
├── local-server.toml
├── data/           ← SQLite 文件
├── frontend/       ← 静态文件
├── lang/           ← i18n
└── log/
```

remote-server 部署结构：
```
/opt/brain-forever-remote/
├── remote-server   ← stub
├── remote-server.toml
└── log/
```

---

## 实施步骤

### Phase 1：目录重组

| # | 操作 | 涉及文件 |
|---|------|---------|
| 1 | 创建 `internal/local/` 目录 | - |
| 2 | `internal/agent/` → `internal/local/agent/`，更新全部 import | ~15 个 .go |
| 3 | `internal/config/` → `internal/local/config/`，更新 import | 1 个 .go |
| 4 | `internal/logger/` → `internal/local/logger/`，更新 import | 1 个 .go |
| 5 | `internal/store/` → `internal/local/store/`，更新 import | 5 个 .go |
| 6 | 创建 `internal/remote/config/` 目录 | - |

### Phase 2：local-server 入口

| # | 操作 | 文件 |
|---|------|------|
| 1 | 创建 `cmd/local-server/main.go` | 从当前 `main.go` 提取，import 改为 `internal/local/*` |
| 2 | 反向代理 `/api/*` → remote-server | 同上 |
| 3 | 前端文件服务 | 同上 |
| 4 | TOML 配置加载 | 同上 |

### Phase 3：remote-server stub

| # | 操作 | 文件 |
|---|------|------|
| 1 | 创建 `cmd/remote-server/main.go` | Hello-World 级：启动 HTTP，返回 `{"status":"ok"}` |
| 2 | TOML 配置加载 | 最小化 |

### Phase 4：构建 & 部署

| # | 操作 |
|---|------|
| 1 | 更新 `b.sh` / `b.bat`：同时构建 `local-server` 和 `remote-server` |
| 2 | 更新 `brain-forever.ps1`：指向 `local-server` |
| 3 | 创建 `deploy/local-server.toml.example` |
| 4 | 创建 `deploy/remote-server.toml.example` |
| 5 | 更新 `.gitignore`：忽略实际配置文件和 binary |

### Phase 5：验证

| # | 验证项 |
|---|--------|
| 1 | `go build ./cmd/local-server/` 编译通过 |
| 2 | `go build ./cmd/remote-server/` 编译通过 |
| 3 | local-server 浏览器可访问，前端正常加载 |
| 4 | remote-server `GET /api/health` 返回正常 |
| 5 | 数据库文件在 `data/` 目录正常读写 |
