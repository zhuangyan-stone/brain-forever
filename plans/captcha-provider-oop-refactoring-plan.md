# Captcha 重构计划：改为点击式图形验证码

## 背景

当前 [`infra/captcha/store.go`](infra/captcha/store.go) 使用**文字型验证码**（从 `index.dat` 加载图片名→文本答案映射，用户输入文字匹配）。

新需求改为**点击式图形验证码**：用户看到验证码图片 + 中文/英文提问，点击图片中特定位置即可，后端验证点击坐标是否在预定矩形区域内。

---

## 核心差异：验证方式的变化

| | 旧（文字型） | 新（点击式） |
|---|---|---|
| 数据 | 文本 code | 坐标矩形 (Left,Top,Right,Bottom) + 问题文本 |
| 用户操作 | 输入看到的文字 | 点击图片特定位置 |
| 验证 | `strings.EqualFold(code, input)` | 检查 (x,y) 是否在矩形内 |
| 前端数据 | 发送文字 | 发送文件名 + 点击坐标 |
| 图片尺寸 | 未规定 | 600×300 (2:1)，前端建议显示 480×240 |

---

## 完整验证流程

```mermaid
sequenceDiagram
    participant F as 前端
    participant B as 后端 user.Handler
    participant CP as CaptchaProvider
    participant R as Redis

    F->>B: GET /api/verify/captcha?action=login
    B->>CP: GetOne()
    CP->>R: HRANDFIELD CAPTCHAS_store.d1
    R-->>CP: field=xxx (图片名)
    CP->>R: HGET CAPTCHAS_store.d1 xxx
    R-->>CP: {"q_cn":"...","q_en":"...","left":100,"top":50,"right":200,"bottom":150}
    CP-->>B: CaptchaItem{Image:"d1/png/xxx.png", Data:{...}}
    B->>R: SET sessionid::captcha::login = CaptchaItem JSON, EX 120
    B-->>F: {src:"/captcha/d1/png/xxx.png", action:"login", q_cn:"...", q_en:"..."}

    Note over F: 用户点击图片某处
    F->>F: 计算点击坐标，映射到原始 600×300 尺寸

    F->>B: POST /api/verify/sms?tel=xxx&action=login<br/>&img_name=xxx&click_x=150&click_y=80
    B->>R: GET sessionid::captcha::login
    R-->>B: CaptchaData{Left:100,Top:50,Right:200,Bottom:150}
    B->>B: 验证 click_x=150 在 [100,200] 区间<br/>验证 click_y=80 在 [50,150] 区间
    Note over B: 验证通过后，发送短信
```

---

## 涉及文件变更清单

```mermaid
flowchart TD
    subgraph "重写"
        S["infra/captcha/store.go<br/>完全重写"]
    end

    subgraph "新增"
        R["infra/captcha/redis_adapter.go<br/>CaptchaStore Redis 实现"]
    end

    subgraph "修改"
        C["internal/config/config.go<br/>CaptchaConfig 字段变更"]
        H["internal/user/handler.go<br/>增加 captchaProvider 字段"]
        L["internal/user/login.go<br/>新增 OnGetVerifyCaptcha<br/>新增坐标验证逻辑"]
        M["cmd/server/main.go<br/>初始化 CaptchaProvider"]
        RT["cmd/server/routers.go<br/>路由指向 user Handler"]
    end

    subgraph "删除"
        HD["infra/captcha/handler.go"]
    end

    S --> R
    S --> C
    S --> H
    S --> L
    S --> M
    S --> RT
    L --> HD
```

---

## 详细步骤

### 步骤 1：修改配置模型 — [`internal/config/config.go`](internal/config/config.go:227)

当前 `CaptchaConfig` 只有一个 `Dir` 字段，需要扩展为：

```go
type CaptchaConfig struct {
    URLBase string // 验证码图片的 URL 访问基础路径，如 "/static/img/captchas/"
    DirBase string // 服务器本地文件系统路径，如 "./frontend/static/img/captchas/"
}
```

`DirBase` 下包含 `d1/` 和 `d2/` 两个子目录，各含 `png/` 和 `json/` 子目录。

注意：注释说 `captchaDirBase` 和 `captchaURLBase` 指向同一目录，只是前者用于服务端读取，后者用于前端 URL 拼接。

---

### 步骤 2：完全重写 [`infra/captcha/store.go`](infra/captcha/store.go)

**删除全部现有代码，仅保留 `package captcha`**。新文件包含：

#### 2a. 数据模型

```go
// CaptchaData 验证码的问题和点击区域答案。
type CaptchaData struct {
    QCn          string `json:"q_cn"`          // 中文提问
    QEn          string `json:"q_en"`          // 英文提问
    Left, Top    int    `json:"left,top"`      // 点击区域左上角
    Right, Bottom int   `json:"right,bottom"`  // 点击区域右下角
}

// CaptchaItem 单个验证码条目。
type CaptchaItem struct {
    Image string      `json:"image"` // 图片 URL 相对路径，如 "d1/png/xxx.png"
    Data  CaptchaData `json:"data"`  // 验证码数据
}
```

#### 2b. CaptchaStore 接口（抽象 Redis 操作）

```go
// CaptchaStore 验证码数据的存储抽象，由调用方提供 Redis 实现。
type CaptchaStore interface {
    HSet(ctx context.Context, key, field string, value interface{}) error
    HGet(ctx context.Context, key, field string) (string, error)
    HRandField(ctx context.Context, key string, count int) ([]string, error)
    Del(ctx context.Context, key ...string) error
}
```

#### 2c. CaptchaProvider struct

```go
type CaptchaProvider struct {
    captchaURLBase string        // 图片 URL 基础路径
    captchaDirBase string        // 本地文件系统路径
    activeDir      string        // 当前活动目录 "d1" 或 "d2"
    store          CaptchaStore  // Redis 存储
    mu             sync.RWMutex  // 保护 activeDir，GetOne/Refresh 并发安全
}
```

#### 2d. 构造函数 `NewCaptchaProvider()`

1. 调用内部 `loadAndStore(ctx, dir, store)` 加载 d1
2. 调用内部 `loadAndStore(ctx, dir, store)` 加载 d2
3. 检查 `d1.active` / `d2.active` 文件，设置 `activeDir`
4. 默认 `activeDir = "d1"`
5. 加载的 map 数据写入 Redis 后丢弃（注释第 5 条）

#### 2e. 内部方法 `loadAndStore(ctx, dir, store)`

此方法供 d1/d2 共用（注释第 3.1/3.2 条要求）：

1. 读取 `{captchaDirBase}/{dir}/png/` 下所有 `.png` 文件 → 提取文件名（无后缀无路径）
2. 读取 `{captchaDirBase}/{dir}/json/` 下所有 `.json` 文件 → `map[filename]jsonBytes`
3. 遍历 PNG 文件名，检查是否存在对应 JSON → 不存在则跳过
4. 将匹配的 `map[filename]jsonBytes` 用 `HSet` 逐条写入 Redis 哈希表，key = `CAPTCHAS_store.{dir}`
5. 丢弃 map 释放内存

#### 2f. `GetOne() (*CaptchaItem, error)` — 核心方法

```mermaid
flowchart LR
    A[GetOne] --> B[RLock 读 activeDir]
    B --> C[Redis key = CAPTCHAS_store.dx]
    C --> D[HRANDFIELD key 1 取随机 field]
    D --> E[HGET key field 取 JSON]
    E --> F[解析 JSON → CaptchaData]
    F --> G[组装 Image 路径: dir/png/field.png]
    G --> H[返回 CaptchaItem]
```

注释第 6 条要求：通过 `activeDir` 组装 Redis key，用 `HRANDFIELD` 随机获取一个 field（图片名），返回图片 URL 和 JSON 数据。

#### 2g. `Refresh(activeDir string) error` — 刷新方法

注释第 7 条要求：
1. `mu.Lock()` 写锁
2. 清空 Redis 中原有数据（`Del CAPTCHAS_store.{oldDir}`）
3. 调用 `loadAndStore(ctx, activeDir, store)` 重新加载
4. 更新 `p.activeDir = activeDir`
5. 解锁

并发模型：`GetOne` 用 `RLock`（多个可并行），`Refresh` 用 `Lock`（与 GetOne 互斥）。

---

### 步骤 3：实现 Redis 适配器 — 新建 [`infra/captcha/redis_adapter.go`](infra/captcha/)

```go
type redisStore struct {
    client *redis.Client
}

func (s *redisStore) HSet(ctx context.Context, key, field string, value interface{}) error {
    return s.client.HSet(ctx, key, field, value).Err()
}
func (s *redisStore) HGet(ctx context.Context, key, field string) (string, error) {
    return s.client.HGet(ctx, key, field).Result()
}
func (s *redisStore) HRandField(ctx context.Context, key string, count int) ([]string, error) {
    return s.client.HRandField(ctx, key, count).Result()
}
func (s *redisStore) Del(ctx context.Context, key ...string) error {
    return s.client.Del(ctx, key...).Err()
}
```

**设计说明**：不直接暴露 `*redis.Client`，定义接口以便测试可 mock。

---

### 步骤 4：迁移 Handler 到 [`internal/user/login.go`](internal/user/login.go)

注释第 8 条明确指出：当前 handler 实现位置错误，应移到 `internal/user`。

#### 4a. 修改 [`internal/user/handler.go`](internal/user/handler.go)

增加字段：

```go
type Handler struct {
    sessionManager  *session.Manager
    cookieName      string
    logger          zylog.Logger
    avatarDir       string
    smsCodeCache    *cache.SMSCodeCache
    captchaProvider *captcha.CaptchaProvider  // 新增
}
```

更新 `NewHandler()` 签名增加 `captchaProvider` 参数。

#### 4b. 在 [`internal/user/login.go`](internal/user/login.go) 实现 `OnGetVerifyCaptcha`

```go
func (h *Handler) OnGetVerifyCaptcha(w http.ResponseWriter, r *http.Request) {
    // 1. 解析 session + action 参数
    // 2. 验证 action 合法性（login/resetpwd）
    // 3. 调用 h.captchaProvider.GetOne() 获取随机验证码
    // 4. 将 CaptchaItem 存入 Redis 缓存
    //    key:   {sessionID}::captcha::{action}
    //    value: CaptchaItem JSON
    //    TTL:   120 秒（2分钟）
    // 5. 返回 JSON:
    //    { src: "/captcha/d1/png/xxx.png", action: "login",
    //      q_cn: "请点击图片中的花朵", q_en: "Please click the flower" }
}
```

#### 4c. 在 `OnGetSMSVerifyCode` 中增加点击坐标验证

当前验证逻辑（第 48 行）：

```go
exists, matches := captcha.VerifyCaptchaCacheEx(sess, action, captchaCode)
```

修改为坐标验证：

```go
func (h *Handler) OnGetSMSVerifyCode(w http.ResponseWriter, r *http.Request) {
    // ... 现有参数解析 ...

    // 新增参数：img_name, click_x, click_y
    imgName := r.URL.Query().Get("img_name")
    clickX := r.URL.Query().Get("click_x")
    clickY := r.URL.Query().Get("click_y")

    // 从 Redis 获取之前存储的 CaptchaItem
    cacheKey := sessionID + "::captcha::" + action
    // ... HGET redis ...

    // 验证：
    // 1. img_name 是否匹配缓存的图片名
    // 2. click_x, click_y 是否在 CaptchaData 的矩形区域内
    //    (left ≤ x ≤ right) && (top ≤ y ≤ bottom)

    // ... 后续 SMS 发送逻辑不变 ...
}
```

---

### 步骤 5：修改 [`cmd/server/main.go`](cmd/server/main.go:156)

将旧的：

```go
if count, err := captcha.Refresh(cfg.Captcha.Dir); err != nil { ... }

captchaHandler := captcha.NewHandler(
    chatHandler.GetSessionManager(),
    chatHandler.GetCookieName(),
)
```

改为：

```go
// 从 ChatAgent 获取 Redis client（如果可用）
var captchaStore captcha.CaptchaStore
if redisClient := chatHandler.GetRedisClient(); redisClient != nil {
    captchaStore = captcha.NewRedisStore(redisClient)
} else {
    // Redis 不可用时，使用内存实现（开发/测试用）
    captchaStore = captcha.NewMemoryStore()
}

captchaProvider := captcha.NewCaptchaProvider(
    cfg.Captcha.URLBase,
    cfg.Captcha.DirBase,
    captchaStore,
)

// 将 captchaProvider 传给 userHandler
userHandler := user.NewHandler(
    chatHandler.GetSessionManager(),
    chatHandler.GetCookieName(),
    chatHandler.GetLogger(),
    chatHandler.GetAvatarDir(),
    chatHandler.GetSMSCodeCache(),
    captchaProvider,  // 新增参数
)
```

需要确认 `ChatAgent` 是否有 `GetRedisClient()` 方法暴露 Redis client。如果没有，需要新增，或者通过其他方式获取。

---

### 步骤 6：修改 [`cmd/server/routers.go`](cmd/server/routers.go:111)

1. 移除 `initRouters` 的 `captchaHandler *captcha.Handler` 参数
2. 将路由注册从：
   ```
   srv.GET("/api/verify/captcha", captchaHandler.OnGetVerifyCaptcha)
   ```
   改为：
   ```
   srv.GET("/api/verify/captcha", userHandler.OnGetVerifyCaptcha)
   ```
3. 移除 `"BrainForever/infra/captcha"` import（如果不再直接引用）

同时移除 `main.go` 中创建 `captchaHandler` 的代码行。

---

### 步骤 7：删除旧文件

删除 [`infra/captcha/handler.go`](infra/captcha/handler.go) — 其功能已迁移到 `internal/user/login.go`。

---

## 综合 Todo 清单

1. **`internal/config/config.go`** — 重构 `CaptchaConfig`：`Dir` → `URLBase` + `DirBase`
2. **`infra/captcha/store.go`** — 完全重写：定义 `CaptchaData` / `CaptchaItem`、`CaptchaStore` 接口、`CaptchaProvider` struct（构造函数、`loadAndStore`、`GetOne`、`Refresh`）
3. **`infra/captcha/redis_adapter.go`**（新建）— 实现 `CaptchaStore` 接口的 Redis 适配器 + 可选的内存实现
4. **`internal/user/handler.go`** — `Handler` 增加 `captchaProvider` 字段，更新 `NewHandler` 签名
5. **`internal/user/login.go`** — 实现 `OnGetVerifyCaptcha`（获取验证码）+ 修改 `OnGetSMSVerifyCode`（坐标验证）
6. **`cmd/server/main.go`** — 初始化 `CaptchaProvider`，传入 `user.NewHandler`
7. **`cmd/server/routers.go`** — `GET /api/verify/captcha` 路由指向 `userHandler.OnGetVerifyCaptcha`，移除 `captchaHandler`
8. **删除 `infra/captcha/handler.go`** — 功能已迁移完毕
