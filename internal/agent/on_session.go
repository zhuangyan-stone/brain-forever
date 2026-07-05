package agent

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/i18n"
)

// ============================================================
// Session handler -GET /api/session
// ============================================================

// OnSession handles GET /api/session
// Creates or retrieves an HTTP session, returning session-level info (user_no + welcome).
// Identified only via cookie http-session-sn, without query parameters.
func (h *ChatAgent) OnSession(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Get localized welcome message based on defaultLang
	welcome := i18n.TL(h.defaultLang, "welcome_message")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_sn": session.userSN,
		"welcome": welcome,
	})
}
