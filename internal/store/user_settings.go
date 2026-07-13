package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// ============================================================
// ApiSetting - external service API config
// ============================================================

// ApiSetting represents an external service API config, including provider and API key.
// Supports bidirectional JSON serialization, compatible with both legacy (plain string) and new (object) formats.
type ApiSetting struct {
	Provider string `json:"provider"` // provider name, e.g. "deepseek", "ali", "zhipu", "bocha"
	ApiKey   string `json:"api_key"`  // API Key
	Private  bool   `json:"private"`  // true if user-provided (private); false if system-shared (billed)
}

// Desensitize masks sensitive ApiKey for frontend display:
//   - Private==true && key==""  → keep as-is (private key not yet set)
//   - Private==true && key!=""  → replace each char with '*'
//   - Private==false            → keep empty (system provides the shared key)
func (a *ApiSetting) Desensitize() {
	if a.Private && a.ApiKey == "" {
		return
	}
	if a.Private {
		a.ApiKey = starifyApiKey(a.ApiKey)
	} else {
		a.ApiKey = ""
	}
}

// IsPseudo returns true if the ApiKey is a starified placeholder ("****").
// This indicates the value came from the frontend's desensitized display
// rather than an actual user modification.
func (a *ApiSetting) IsPseudo() bool {
	if a.ApiKey == "" {
		return false
	}
	for _, r := range a.ApiKey {
		if r != '*' {
			return false
		}
	}
	return true
}

// starifyApiKey replaces each character in s with '*'.
func starifyApiKey(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	for i := range runes {
		runes[i] = '*'
	}
	return string(runes)
}

// UnmarshalJSON supports two JSON formats:
//   - Legacy: plain string "sk-xxx"
//   - New: object {"provider":"deepseek","api_key":"sk-xxx"}
func (a *ApiSetting) UnmarshalJSON(data []byte) error {
	// Try legacy format (plain string)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		a.Provider = ""
		a.ApiKey = s
		return nil
	}

	// Try new format (object)
	type Alias ApiSetting
	var alias Alias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*a = ApiSetting(alias)
	return nil
}

// MarshalJSON serializes to the new (object) format.
// Legacy data read from DB is automatically upgraded on next write.
func (a ApiSetting) MarshalJSON() ([]byte, error) {
	type Alias ApiSetting
	return json.Marshal(Alias(a))
}

// ============================================================
// UserSettingsTheme - theme preferences
// ============================================================

// UserSettingsTheme holds theme preferences for the UI.
type UserSettingsTheme struct {
	Active string `json:"active"` // Active theme mode: "light", "dark", or "system" (empty defaults to "system")
	Light  string `json:"light"`  // Light theme ID
	Dark   string `json:"dark"`   // Dark theme ID
	Sync   bool   `json:"sync"`   // Whether to sync theme across devices
}

// ============================================================
// UserSettingsAPIKey - collection of API configurations for external services
// ============================================================

// UserSettingsAPIKey holds API configurations for external services.
type UserSettingsAPIKey struct {
	LLM      ApiSetting `json:"llm"`      // LLM service (chat & trait extraction)
	Search   ApiSetting `json:"search"`   // Web search service
	Embedder ApiSetting `json:"embedder"` // Embedder service
}

// IsOK returns true when all three API keys (LLM, Search, Embedder) are non-empty.
func (a *UserSettingsAPIKey) IsOK() bool {
	return a.LLM.ApiKey != "" && a.Search.ApiKey != "" && a.Embedder.ApiKey != ""
}

// ============================================================
// UserSettings - user settings (JSON format, stored in users.settings column)
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
// Normalizes Theme.Active: empty string is treated as "system".
func (s *UserStore) GetUserSettings(id int64) (*UserSettings, error) {
	sqlStr := "SELECT settings FROM users WHERE id = $1"
	var jsonStr string
	err := ThePGDB().Get(&jsonStr, sqlStr, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found (id=%d)", id)
		}
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return nil, fmt.Errorf("failed to query user settings: %w", err)
	}

	var settings UserSettings
	if err := settings.FromString(jsonStr); err != nil {
		return nil, fmt.Errorf("failed to parse user settings: %w", err)
	}

	// Normalize: empty Active defaults to "system"
	if settings.Theme.Active == "" {
		settings.Theme.Active = "system"
	}
	return &settings, nil
}

// SetUserSettings writes the full UserSettings for a given user.
// Serializes the settings to JSON and updates the settings column.
func (s *UserStore) SetUserSettings(id int64, settings *UserSettings) error {
	jsonStr := settings.ToString()

	sqlStr := "UPDATE users SET settings = $1::jsonb WHERE id = $2"
	result, err := ThePGDB().Exec(sqlStr, jsonStr, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
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
// active should be one of "light", "dark", or "system".
// Uses MySQL JSON_SET to update only the $.theme.active field in-place.
func (s *UserStore) UpdateThemeActiveMode(id int64, active string) error {
	sqlStr := "UPDATE users SET settings = jsonb_set(settings, '{theme,active}', to_jsonb($1::text)) WHERE id = $2"
	result, err := ThePGDB().Exec(sqlStr, active, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to update user settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found (id=%d)", id)
	}
	return nil
}

// UpdateThemeSyncMode updates the $.theme.sync field for a user.
// Uses MySQL JSON_SET to update only the $.theme.sync field in-place.
// sync is true to enable cross-device theme sync, false to disable.
func (s *UserStore) UpdateThemeSyncMode(id int64, sync bool) error {
	sqlStr := "UPDATE users SET settings = jsonb_set(settings, '{theme,sync}', to_jsonb($1)) WHERE id = $2"
	result, err := ThePGDB().Exec(sqlStr, sync, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to update theme sync mode: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found (id=%d)", id)
	}
	return nil
}

// ============================================================
// UserStore API key-related methods
// ============================================================

// UpdateUserSettingsAPIKey updates the API key settings for a user.
// It serializes the apis struct to JSON and updates $.api_key in-place.
func (s *UserStore) UpdateUserSettingsAPIKey(id int64, apis *UserSettingsAPIKey) error {
	jsonBytes, err := json.Marshal(apis)
	if err != nil {
		return fmt.Errorf("failed to marshal API key settings: %w", err)
	}

	sqlStr := "UPDATE users SET settings = jsonb_set(settings, '{api_key}', $1::jsonb) WHERE id = $2"
	result, err := ThePGDB().Exec(sqlStr, string(jsonBytes), id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to update user API key settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found (id=%d)", id)
	}
	return nil
}

// UpdateThemes updates the light/dark theme IDs and the active mode for a user.
// light is the light theme ID, dark is the dark theme ID.
// active should be one of "light", "dark", or "system".
// Uses MySQL JSON_SET to update all three $.theme.* fields in a single call.
func (s *UserStore) UpdateThemes(id int64, light, dark, active string) error {
	sqlStr := `UPDATE users SET settings = jsonb_set(
		jsonb_set(
			jsonb_set(settings, '{theme,light}', to_jsonb($1::text)),
			'{theme,dark}', to_jsonb($2::text)
		),
		'{theme,active}', to_jsonb($3::text)
	) WHERE id = $4`
	result, err := ThePGDB().Exec(sqlStr, light, dark, active, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to update user settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found (id=%d)", id)
	}
	return nil
}
