package user

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/i18n"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
)

// ============================================================
// POST /api/user/theme/apply — ApplyTheme
// ============================================================

// ApplyTheme handles POST /api/user/theme/apply.
// It updates the current user's theme preferences in the MySQL users table
// and syncs the in-memory session state.
// Authentication required — the caller should wrap this with RequireAuth.
func (h *Handler) ApplyTheme(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actived      string `json:"actived"`
		ActivedLight string `json:"actived-light"`
		ActivedDark  string `json:"actived-dark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	// Validate and normalize the active mode before saving
	active, valid := normalizeActiveMode(req.Actived)
	if !valid {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "invalid actived value"}), http.StatusBadRequest)
		return
	}

	// Resolve the current user from the session
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	userID := sess.User.ID

	if err := store.TheUserStore().UpdateThemes(userID, req.ActivedLight, req.ActivedDark, active); err != nil {
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	// Sync the in-memory session state
	sess.Mu.Lock()
	sess.User.Settings.Theme = store.UserSettingsTheme{
		Active: active,
		Light:  req.ActivedLight,
		Dark:   req.ActivedDark,
		Sync:   sess.User.Settings.Theme.Sync, // preserve existing sync flag
	}
	sess.Mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// ============================================================
// PUT /api/user/theme/mode — ApplyThemeMode
// ============================================================

// ApplyThemeMode handles PUT /api/user/theme/mode.
// It updates the user's active theme mode (light/dark/system).
//   - mode: 0=light, 1=dark, 2=system
//
// Authentication required — the caller should wrap this with RequireAuth.
func (h *Handler) ApplyThemeMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode int `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	// Map mode int to active string
	activeMap := map[int]string{0: "light", 1: "dark", 2: "system"}
	active, ok := activeMap[req.Mode]
	if !ok {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "invalid mode, must be 0(light), 1(dark), or 2(system)"}), http.StatusBadRequest)
		return
	}

	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	userID := sess.User.ID
	if err := store.TheUserStore().UpdateThemeActiveMode(userID, active); err != nil {
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	// Sync the in-memory session state
	sess.Mu.Lock()
	sess.User.Settings.Theme.Active = active
	sess.Mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// ============================================================
// PUT /api/user/theme/sync — ApplyThemeSync
// ============================================================

// ApplyThemeSync handles PUT /api/user/theme/sync.
// It updates the user's theme sync preference and optionally syncs the
// current theme settings to the server.
//   - sync:  true/false (required) — enable/disable cross-device theme sync
//   - theme: *UserSettingsTheme (optional) — current frontend theme settings.
//     Must be provided when sync=true, must be nil when sync=false.
//
// Authentication required — the caller should wrap this with RequireAuth.
func (h *Handler) ApplyThemeSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Theme *store.UserSettingsTheme `json:"theme"`
		Sync  bool                     `json:"sync"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	// Validation
	if !req.Sync && req.Theme != nil {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "theme must be nil when sync is false"}), http.StatusBadRequest)
		return
	}
	if req.Sync && req.Theme == nil {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "theme is required when sync is true"}), http.StatusBadRequest)
		return
	}

	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	userID := sess.User.ID

	// Save theme settings to DB when provided (sync must be true)
	if req.Theme != nil {
		if err := store.TheUserStore().UpdateThemes(userID, req.Theme.Light, req.Theme.Dark, req.Theme.Active); err != nil {
			http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
			return
		}
		// Sync the in-memory session state
		sess.Mu.Lock()
		sess.User.Settings.Theme = *req.Theme
		sess.Mu.Unlock()
	}

	// Update sync mode
	if err := store.TheUserStore().UpdateThemeSyncMode(userID, req.Sync); err != nil {
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}
	sess.Mu.Lock()
	sess.User.Settings.Theme.Sync = req.Sync
	sess.Mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// normalizeActiveMode converts a raw actived value to a canonical value.
// Accepts: "0"/"light", "1"/"dark", "2"/"system".
// Empty or unknown values default to "system".
func normalizeActiveMode(raw string) (string, bool) {
	switch raw {
	case "0", "light":
		return "light", true
	case "1", "dark":
		return "dark", true
	case "2", "system":
		return "system", true
	default:
		return "system", false
	}
}

// ============================================================
// GET /api/user/theme — GetTheme
// ============================================================

// GetTheme handles GET /api/user/theme.
// It returns the current user's theme preferences (without API keys).
// Authentication required — the caller should wrap this with RequireAuth.
func (h *Handler) GetTheme(w http.ResponseWriter, r *http.Request) {
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	userID := sess.User.ID
	settings, err := store.TheUserStore().GetUserSettings(userID)
	if err != nil {
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings.Theme)
}
