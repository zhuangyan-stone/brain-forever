package agent

import (
	"encoding/json"
	"net/http"
)

// ============================================================
// Logout handler -POST /api/chat/logout
// ============================================================

// OnLogout handles POST /api/chat/logout -switches the current session
// back to anonymous user, clearing the logged-in state.
func (h *ChatAgent) OnLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Switch the session back to anonymous user
	session.switchToUser("")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
