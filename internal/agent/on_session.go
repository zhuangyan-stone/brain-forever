package agent

import (
	"encoding/json"
	"net/http"
)

// ============================================================
// Session handler — GET /api/session
// ============================================================

// OnRestoreSession handles GET /api/session — returns current session info
func (h *ChatHandler) OnRestoreSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, isNew := h.getSessionID(w, r)

	var history []Message

	if !isNew {
		history = h.sessionManager.GetHistory(sessionID)
	}

	resp := map[string]interface{}{
		"session_id": sessionID,
		"is_new":     isNew,
		"history":    history,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
