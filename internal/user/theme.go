package user

import (
	"encoding/json"
	"net/http"

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
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Validate and normalize the active mode before saving
	active, valid := normalizeActiveMode(req.Actived)
	if !valid {
		http.Error(w, `{"error":"invalid actived value, must be light/dark/system or 0/1/2"}`, http.StatusBadRequest)
		return
	}

	// Resolve the current user from the session
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	userID := sess.User.ID

	if err := store.TheUserStore().UpdateThemes(userID, req.ActivedLight, req.ActivedDark, active); err != nil {
		http.Error(w, `{"error":"failed to save theme preferences"}`, http.StatusInternalServerError)
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

// normalizeActiveMode converts a raw actived value to a canonical value.
// Accepts: "0"/"light", "1"/"dark", "2"/"system".
// Returns the canonical value and whether it's valid.
func normalizeActiveMode(raw string) (string, bool) {
	switch raw {
	case "0", "light":
		return "light", true
	case "1", "dark":
		return "dark", true
	case "2", "system":
		return "system", true
	default:
		return "light", false
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
		http.Error(w, `{"error":"failed to get user settings"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings.Theme)
}

// ============================================================
// PUT /api/user/theme/mode — UpdateSyncMode
// ============================================================

// UpdateSyncMode handles PUT /api/user/theme/mode.
// It updates the user's theme sync preference (enable/disable cross-device sync).
// Authentication required — the caller should wrap this with RequireAuth.
func (h *Handler) UpdateSyncMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sync bool `json:"sync"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	userID := sess.User.ID
	if err := store.TheUserStore().UpdateThemeSyncMode(userID, req.Sync); err != nil {
		http.Error(w, `{"error":"failed to update sync mode"}`, http.StatusInternalServerError)
		return
	}

	// Sync the in-memory session state
	sess.Mu.Lock()
	sess.User.Settings.Theme.Sync = req.Sync
	sess.Mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
