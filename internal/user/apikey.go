package user

import (
	"encoding/json"
	"net/http"

	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
)

// ============================================================
// API-Key settings handler — POST /api/user/settings/apikey
// ============================================================

// OnSaveApiKeySettings handles POST /api/user/settings/apikey.
// It saves the user's API key settings (LLM, Search, Embedder) to the database.
// Authentication required — the caller should wrap this with RequireAuth.
func (h *Handler) OnSaveApiKeySettings(w http.ResponseWriter, r *http.Request) {
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	if sess.User.ID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var apis store.UserSettingsAPIKey
	if err := json.NewDecoder(r.Body).Decode(&apis); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Update the session's in-memory settings
	sess.Mu.Lock()
	sess.User.Settings.APIKey = apis
	sess.Mu.Unlock()

	// Persist to database
	if err := store.TheUserStore().UpdateUserSettingsAPIKey(sess.User.ID, &apis); err != nil {
		h.logger.Errorf("failed to save API key settings for user %d: %v", sess.User.ID, err)
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	// Update Redis login session if available
	if h.sessionManager.HasRedis() {
		settingsStr := sess.User.Settings.ToString()
		if err := h.sessionManager.Redis().SetLoginSession(
			h.sessionManager.Ctx, sessionID,
			&cache.LoginSessionData{
				UserID:   sess.User.ID,
				UserSN:   sess.User.SN,
				No:       sess.User.No,
				Nickname: sess.User.Nickname,
				Settings: settingsStr,
			},
		); err != nil {
			h.logger.Warnf("failed to update Redis login session after API key save: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
