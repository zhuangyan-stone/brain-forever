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
// Also removes the login state from Redis (if available).
func (h *ChatAgent) OnLogout(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Clear session state (pass 0, empty sn, nil chats, zero settings = logout)
	session.switchToUser(0, "", nil, store.UserSettings{})

	// Also notify UserStore
	store.TheUserStore().Logout(session.user.SN)

	// Remove login state from Redis (if available)
	if h.sessionManager.redis != nil {
		if err := h.sessionManager.redis.DelLoginSession(h.sessionManager.ctx, sessionID); err != nil {
			h.logger.Warnf("failed to remove login session from Redis: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
