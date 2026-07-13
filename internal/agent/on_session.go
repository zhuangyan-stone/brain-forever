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
	json.NewEncoder(w).Encode(map[string]any{
		"user_sn":  sess.User.SN,
		"no":       sess.User.No,
		"nickname": sess.User.Nickname,
		"welcome":  welcome,
	})
}

// ============================================================
// Session info handler -GET /api/info/sessions
// ============================================================

// OnSessionsScanInfo handles GET /api/info/sessions -returns GC stats from Redis.
func (h *ChatAgent) OnSessionsScanInfo(w http.ResponseWriter, r *http.Request) {
	stats, err := h.sessionManager.Redis().GetGCStats(r.Context())
	if err != nil {
		http.Error(w, "failed to read GC stats", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if stats == nil {
		json.NewEncoder(w).Encode(map[string]string{
			"status": "no_data",
		})
		return
	}
	json.NewEncoder(w).Encode(stats)
}
