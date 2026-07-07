package store

import (
	"database/sql"
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

// ============================================================
// UserStore settings general methods
// ============================================================

// GetUserSettings retrieves the full UserSettings for a given user.
// Returns nil if the user is not found.
func (s *UserStore) GetUserSettings(id int64) (*UserSettings, error) {
	var jsonStr string
	err := TheMySQLDB().Get(&jsonStr, "SELECT settings FROM users WHERE id = ?", id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found (id=%d)", id)
		}
		return nil, fmt.Errorf("failed to query user settings: %w", err)
	}

	var settings UserSettings
	if err := settings.FromString(jsonStr); err != nil {
		return nil, fmt.Errorf("failed to parse user settings: %w", err)
	}
	return &settings, nil
}

// SetUserSettings writes the full UserSettings for a given user.
// Serializes the settings to JSON and updates the settings column.
func (s *UserStore) SetUserSettings(id int64, settings *UserSettings) error {
	jsonStr := settings.ToString()

	result, err := TheMySQLDB().Exec("UPDATE users SET settings = ? WHERE id = ?", jsonStr, id)
	if err != nil {
		return fmt.Errorf("failed to update user settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found (id=%d)", id)
	}
	return nil
}

// ============================================================
// UserStore theme-related methods
// ============================================================

// UpdateThemeActiveMode updates the active theme mode for a user.
// If isDark is true, sets Active to "dark"; otherwise sets it to "light".
// Uses MySQL JSON_SET to update only the $.theme.active field in-place.
func (s *UserStore) UpdateThemeActiveMode(id int64, isDark bool) error {
	activeMode := "light"
	if isDark {
		activeMode = "dark"
	}

	result, err := TheMySQLDB().Exec(
		"UPDATE users SET settings = JSON_SET(settings, '$.theme.active', ?) WHERE id = ?",
		activeMode, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update user settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found (id=%d)", id)
	}
	return nil
}

// UpdateThemes updates the light/dark theme IDs and the active mode for a user.
// light is the light theme ID, dark is the dark theme ID.
// If isDark is true, sets Active to "dark"; otherwise sets it to "light".
// Uses MySQL JSON_SET to update all three $.theme.* fields in a single call.
func (s *UserStore) UpdateThemes(id int64, light, dark string, isDark bool) error {
	activeMode := "light"
	if isDark {
		activeMode = "dark"
	}

	result, err := TheMySQLDB().Exec(
		"UPDATE users SET settings = JSON_SET(settings, '$.theme.light', ?, '$.theme.dark', ?, '$.theme.active', ?) WHERE id = ?",
		light, dark, activeMode, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update user settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found (id=%d)", id)
	}
	return nil
}
