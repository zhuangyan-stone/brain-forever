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
func (h *ChatAgent) OnSession(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	welcome := i18n.TL(h.defaultLang, "welcome_message")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_sn": sess.User.SN,
		"no":      sess.User.No,
		"welcome": welcome,
	})
}
