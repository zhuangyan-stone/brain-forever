package theme

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"

	"BrainForever/infra/i18n"
	"BrainForever/toolset"

	"github.com/BurntSushi/toml"
)

// Handler handles theme-related API requests (GET/POST /api/themes).
type Handler struct {
	themeConfigFile string
	mu              sync.Mutex
}

// NewHandler creates a new theme Handler.
func NewHandler() *Handler {
	return &Handler{
		themeConfigFile: "./local-server.toml",
	}
}

// ThemeRuntime holds the runtime theme config read from / written to local-server.toml.
type ThemeRuntime struct {
	Theme struct {
		Actived      string `toml:"actived"`
		ActivedLight string `toml:"actived-light"`
		ActivedDark  string `toml:"actived-dark"`
	} `toml:"theme"`
}

// readThemeConfig reads the theme runtime config from local-server.toml.
// If the file doesn't exist or is malformed, returns sensible defaults.
func (h *Handler) readThemeConfig() ThemeRuntime {
	var rt ThemeRuntime
	if _, err := toml.DecodeFile(h.themeConfigFile, &rt); err != nil {
		rt.Theme.Actived = "light"
		rt.Theme.ActivedLight = ""
		rt.Theme.ActivedDark = ""
	}
	return rt
}

// writeThemeConfig writes the theme runtime config to local-server.toml.
// Uses a sync.Mutex to prevent concurrent writes.
func (h *Handler) writeThemeConfig(rt ThemeRuntime) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Read existing file to preserve other sections (e.g. server config)
	type fullConfig struct {
		Theme struct {
			Actived      string `toml:"actived"`
			ActivedLight string `toml:"actived-light"`
			ActivedDark  string `toml:"actived-dark"`
		} `toml:"theme"`
	}
	var cfg fullConfig
	if _, err := toml.DecodeFile(h.themeConfigFile, &cfg); err != nil {
		// File doesn't exist or is empty; start fresh
		cfg = fullConfig{}
	}
	cfg.Theme = rt.Theme

	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(h.themeConfigFile, []byte(buf.String()), 0644)
}

// GetThemes handles GET /api/themes.
// Builds the merged response:
//   - themes[] from manifest.json (source code)
//   - actived/actived-light/actived-dark from local-server.toml (runtime config)
func (h *Handler) GetThemes(w http.ResponseWriter, r *http.Request) {
	// Read themes list from manifest.json (source code)
	manifestRaw, err := os.ReadFile("./frontend/themes/manifest.json")
	if err != nil {
		toolset.WriteJSONError(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}
	var manifest struct {
		Themes []any `json:"themes"`
	}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		toolset.WriteJSONError(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	// Read runtime config from local-server.toml
	rt := h.readThemeConfig()

	// Merge into response
	resp := map[string]any{
		"themes":        manifest.Themes,
		"actived":       rt.Theme.Actived,
		"actived-light": rt.Theme.ActivedLight,
		"actived-dark":  rt.Theme.ActivedDark,
		"description":   "BrainGo External Theme List",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SetThemes handles POST /api/themes.
// Updates the theme runtime config (actived/actived-light/actived-dark) in local-server.toml.
// Never touches manifest.json.
func (h *Handler) SetThemes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actived      string `json:"actived"`
		ActivedLight string `json:"actived-light"`
		ActivedDark  string `json:"actived-dark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteJSONError(w, i18n.T("api_error_failed_to_parse_request"), http.StatusBadRequest)
		return
	}

	// Only write to local-server.toml -- never touch manifest.json
	rt := ThemeRuntime{}
	rt.Theme.Actived = req.Actived
	rt.Theme.ActivedLight = req.ActivedLight
	rt.Theme.ActivedDark = req.ActivedDark

	if err := h.writeThemeConfig(rt); err != nil {
		toolset.WriteJSONError(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
