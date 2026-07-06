package store

import (
	"encoding/json"
	"fmt"
)

// ============================================================
// ApiSetting - 外部服务的 API 配置
// ============================================================

// ApiSetting 表示一个外部服务的 API 配置，包含服务提供商和 API Key。
// 支持 JSON 双向序列化，兼容旧格式（纯字符串）和新格式（对象）。
type ApiSetting struct {
	Provider string `json:"provider"` // 服务提供商，如 "deepseek", "ali", "zhipu", "bocha"
	ApiKey   string `json:"api_key"`  // API Key
}

// UnmarshalJSON 兼容两种 JSON 格式：
//   - 旧格式：纯字符串 "sk-xxx"
//   - 新格式：对象 {"provider":"deepseek","api_key":"sk-xxx"}
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

// MarshalJSON 序列化为新格式（对象）。
// 旧格式数据读取后，下次写入时会自动升级为新格式。
func (a ApiSetting) MarshalJSON() ([]byte, error) {
	type Alias ApiSetting
	return json.Marshal(Alias(a))
}

// ============================================================
// UserSettingsTheme - 主题偏好
// ============================================================

// UserSettingsTheme holds theme preferences for the UI.
type UserSettingsTheme struct {
	Active string `json:"active"` // Active theme mode: "light" or "dark"
	Light  string `json:"light"`  // Light theme ID
	Dark   string `json:"dark"`   // Dark theme ID
}

// ============================================================
// UserSettingsAPIKey - 外部服务的 API 配置集合
// ============================================================

// UserSettingsAPIKey holds API configurations for external services.
type UserSettingsAPIKey struct {
	LLM      ApiSetting `json:"llm"`      // LLM service (chat & trait extraction)
	Search   ApiSetting `json:"search"`   // Web search service
	Embedder ApiSetting `json:"embedder"` // Embedder service
}

// ============================================================
// UserSettings - 用户设置（JSON 格式，存储在 users.settings 列）
// ============================================================

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
// Returns an error if the JSON is invalid.
// If jsonStr is empty, initializes with default values.
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
// Returns an empty string on marshal error (should not happen with valid data).
func (s *UserSettings) ToString() string {
	data, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(data)
}
