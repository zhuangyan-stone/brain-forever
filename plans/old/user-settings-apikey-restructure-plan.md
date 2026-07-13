# UserSettings APIKey 结构调整方案

## 1. 现状

### [`internal/store/users.go:24`](internal/store/users.go:24)
```go
type UserSettings struct {
    V      int                `json:"v"`
    APIKey UserSettingsAPIKey `json:"api_key"`
    Theme  UserSettingsTheme  `json:"theme"`
}

type UserSettingsAPIKey struct {
    LLM      string `json:"llm"`      // 纯字符串 API Key
    Search   string `json:"search"`
    Embedder string `json:"embedder"`
}
```

### [`internal/store/user_settings.go:3`](internal/store/user_settings.go:3)（新建）
```go
type ApiSetting struct {
    Provider string
    ApiKey   string
}
```

## 2. 目标

将 `UserSettingsAPIKey` 的三个字段从纯字符串改为 `ApiSetting` 结构体，使每个服务可以指定 **Provider + APIKey** 两部分。

改造后：
```go
type UserSettingsAPIKey struct {
    LLM      ApiSetting `json:"llm"`
    Search   ApiSetting `json:"search"`
    Embedder ApiSetting `json:"embedder"`
}
```

JSON 存储格式变化：
```json
// 旧格式 (V=0)
{
  "v": 0,
  "api_key": {
    "llm": "sk-xxx",
    "search": "",
    "embedder": ""
  }
}

// 新格式 (V=1)
{
  "v": 1,
  "api_key": {
    "llm": { "provider": "deepseek", "api_key": "sk-xxx" },
    "search": { "provider": "", "api_key": "" },
    "embedder": { "provider": "ali", "api_key": "" }
  }
}
```

## 3. 兼容性方案

**关键问题**：MySQL 中已存在的用户 settings JSON 是旧格式（纯字符串），直接反序列化到新结构体会失败。

**方案**：在 `FromString` 中兼容两种格式，利用 V 字段区分：

```go
func (s *UserSettings) FromString(jsonStr string) error {
    if jsonStr == "" {
        s.init()
        return nil
    }

    // 尝试按新格式解析
    if err := json.Unmarshal([]byte(jsonStr), s); err == nil {
        // 检查是否是新格式：APIKey 各字段如果是 string 说明是旧格式
        // 利用中间 raw 检测
    }

    // 尝试按旧格式解析
    // ...
}
```

**更好的方案**：使用 `json.RawMessage` 做两阶段反序列化：

```go
func (s *UserSettings) FromString(jsonStr string) error {
    if jsonStr == "" {
        s.init()
        return nil
    }

    // 第一阶段：解析外层结构，APIKey 用 RawMessage 暂存
    var raw struct {
        V      int              `json:"v"`
        APIKey json.RawMessage  `json:"api_key"`
        Theme  UserSettingsTheme `json:"theme"`
    }
    if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
        s.init()
        return nil
    }

    s.V = raw.V
    s.Theme = raw.Theme

    if raw.APIKey != nil {
        // 尝试按新格式解析
        var newFormat UserSettingsAPIKey
        if err := json.Unmarshal(raw.APIKey, &newFormat); err == nil {
            // 检查是否真的是新格式（LLM 字段不是 string）
            if !isLegacyFormat(raw.APIKey) {
                s.APIKey = newFormat
                return nil
            }
        }

        // 回退：按旧格式解析（三个 string）
        var legacy struct {
            LLM      string `json:"llm"`
            Search   string `json:"search"`
            Embedder string `json:"embedder"`
        }
        if err := json.Unmarshal(raw.APIKey, &legacy); err == nil {
            s.APIKey = UserSettingsAPIKey{
                LLM:      ApiSetting{Provider: "", ApiKey: legacy.LLM},
                Search:   ApiSetting{Provider: "", ApiKey: legacy.Search},
                Embedder: ApiSetting{Provider: "", ApiKey: legacy.Embedder},
            }
            s.V = 1 // 升级到新版本
        }
    }

    return nil
}
```

但这样比较复杂。**更简洁的方案**：直接定义 `ApiSetting` 的 `UnmarshalJSON` 方法，让它既能接受字符串 `"sk-xxx"` 也能接受对象 `{"provider":"deepseek","api_key":"sk-xxx"}`：

```go
func (a *ApiSetting) UnmarshalJSON(data []byte) error {
    // 先尝试解析为字符串（旧格式）
    var s string
    if err := json.Unmarshal(data, &s); err == nil {
        a.Provider = ""
        a.ApiKey = s
        return nil
    }

    // 再尝试解析为对象（新格式）
    type Alias ApiSetting
    var alias Alias
    if err := json.Unmarshal(data, &alias); err != nil {
        return err
    }
    *a = ApiSetting(alias)
    return nil
}

// 序列化时统一输出新格式（对象）
func (a ApiSetting) MarshalJSON() ([]byte, error) {
    type Alias ApiSetting
    return json.Marshal(Alias(a))
}
```

**推荐方案**：使用 `UnmarshalJSON` 兼容旧格式。这样好处是：
- `FromString`/`ToString` 无需改动
- 旧格式自动兼容
- 序列化时统一输出新格式（升级持久化）

## 4. 文件变更

### 4.1 [`internal/store/user_settings.go`](internal/store/user_settings.go)

```go
package store

import "encoding/json"

// ApiSetting 表示一个外部服务的 API 配置
type ApiSetting struct {
    Provider string `json:"provider"`  // 服务提供商，如 "deepseek", "ali", "zhipu", "bocha"
    ApiKey   string `json:"api_key"`   // API Key
}

// UnmarshalJSON 兼容旧格式（纯字符串）和新格式（对象）
func (a *ApiSetting) UnmarshalJSON(data []byte) error {
    // 先尝试解析为字符串（旧格式）
    var s string
    if err := json.Unmarshal(data, &s); err == nil {
        a.Provider = ""
        a.ApiKey = s
        return nil
    }

    // 再尝试解析为对象（新格式）
    type Alias ApiSetting
    var alias Alias
    if err := json.Unmarshal(data, &alias); err != nil {
        return err
    }
    *a = ApiSetting(alias)
    return nil
}

// MarshalJSON 序列化为新格式（对象）
func (a ApiSetting) MarshalJSON() ([]byte, error) {
    type Alias ApiSetting
    return json.Marshal(Alias(a))
}
```

### 4.2 [`internal/store/users.go`](internal/store/users.go)

**修改**：
1. 从 `users.go` 中移除 `UserSettings`、`UserSettingsAPIKey`、`UserSettingsTheme` 的定义
2. 移动 `init()`、`FromString()`、`ToString()` 方法到 `user_settings.go`
3. 更新 `UserSettingsAPIKey` 的字段类型

```go
// users.go 中移除以下代码块（24-81行）
```

`users.go` 中保留 `import` 无需新增，因为 `UserSettings` 在同一 package 中。

### 4.3 [`internal/store/user_settings.go`](internal/store/user_settings.go)（完整内容）

```go
package store

import (
    "encoding/json"
    "fmt"
)

// ApiSetting 表示一个外部服务的 API 配置
type ApiSetting struct {
    Provider string `json:"provider"`  // 服务提供商，如 "deepseek", "ali", "zhipu", "bocha"
    ApiKey   string `json:"api_key"`   // API Key
}

// UnmarshalJSON 兼容旧格式（纯字符串）和新格式（对象）
func (a *ApiSetting) UnmarshalJSON(data []byte) error {
    var s string
    if err := json.Unmarshal(data, &s); err == nil {
        a.Provider = ""
        a.ApiKey = s
        return nil
    }
    type Alias ApiSetting
    var alias Alias
    if err := json.Unmarshal(data, &alias); err != nil {
        return err
    }
    *a = ApiSetting(alias)
    return nil
}

// MarshalJSON 序列化为新格式（对象）
func (a ApiSetting) MarshalJSON() ([]byte, error) {
    type Alias ApiSetting
    return json.Marshal(Alias(a))
}

// UserSettingsTheme holds theme preferences for the UI.
type UserSettingsTheme struct {
    Active string `json:"active"`
    Light  string `json:"light"`
    Dark   string `json:"dark"`
}

// UserSettingsAPIKey holds API configurations for external services.
type UserSettingsAPIKey struct {
    LLM      ApiSetting `json:"llm"`      // LLM service (chat & trait extraction)
    Search   ApiSetting `json:"search"`   // Web search service
    Embedder ApiSetting `json:"embedder"` // Embedder service
}

// UserSettings represents user settings in JSON format, stored in the settings field of User.
type UserSettings struct {
    V      int                `json:"v"`       // Settings version
    APIKey UserSettingsAPIKey `json:"api_key"` // API configurations
    Theme  UserSettingsTheme  `json:"theme"`   // Theme preferences
}

// Init sets UserSettings to its default values.
func (s *UserSettings) Init() {
    s.V = 0
    s.APIKey = UserSettingsAPIKey{}
    s.Theme = UserSettingsTheme{}
}

// FromString parses a JSON string into UserSettings.
func (s *UserSettings) FromString(jsonStr string) error {
    if jsonStr == "" {
        s.Init()
        return nil
    }
    if err := json.Unmarshal([]byte(jsonStr), s); err != nil {
        return fmt.Errorf("failed to parse UserSettings JSON: %w", err)
    }
    return nil
}

// ToString serializes UserSettings to a JSON string.
func (s *UserSettings) ToString() string {
    data, err := json.Marshal(s)
    if err != nil {
        return ""
    }
    return string(data)
}
```

注意：`init()` → `Init()`（导出），因为 `user_settings.go` 中的 `Init` 方法需要被外部引用（虽然不是必须导出，但统一风格）。

## 5. 影响范围

| 引用点 | 文件 | 影响 |
|--------|------|------|
| `UserSettings` | [`internal/store/users.go:24`](internal/store/users.go:24) | 移除定义，类型仍在同包中可用 |
| `UserSettings.init()` | [`internal/store/users.go:45`](internal/store/users.go:45) | 改为 `Init()`，更新调用 |
| `UserSettings.FromString` | [`internal/store/users.go:59`](internal/store/users.go:59) | 移动代码 |
| `UserSettings.ToString` | [`internal/store/users.go:72`](internal/store/users.go:72) | 移动代码 |
| 所有引用 `users.UserSettings` 的地方 | 全局 | 不受影响（同包） |

搜索确认：`UserSettingsAPIKey` 仅在本文件中被引用，无外部调用点。

## 6. 实施步骤

1. 重写 [`internal/store/user_settings.go`](internal/store/user_settings.go) — 包含所有 UserSettings 类型定义 + 方法
2. 从 [`internal/store/users.go`](internal/store/users.go) 中移除 24-81 行（UserSettings 相关定义和 init/FromString/ToString）
3. 编译验证：`go build ./internal/store/`
4. 全局编译验证：`go build -o brain-forever ./cmd/server/`
