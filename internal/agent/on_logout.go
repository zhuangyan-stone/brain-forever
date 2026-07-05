package agent

import (
	"BrainForever/internal/store"
	"encoding/json"
	"net/http"
)

// ============================================================
// Logout handler -POST /api/user/logout
// ============================================================

// OnLogout handles POST /api/user/logout -clears the current session's
// user state, returning it to an unauthenticated state.
func (h *ChatAgent) OnLogout(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Clear session state (pass empty sn and nil chats = logout)
	session.switchToUser("", nil)

	// Also notify UserStore
	store.TheUserStore().Logout(session.userSN)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
