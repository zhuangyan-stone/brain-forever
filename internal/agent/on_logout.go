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
	sess := h.sessionManager.GetOrCreate(sessionID)

	sess.SwitchToUser(0, "", nil, store.UserSettings{})

	store.TheUserStore().Logout(sess.User.SN)

	if h.sessionManager.Redis != nil {
		if err := h.sessionManager.Redis.DelLoginSession(h.sessionManager.Ctx, sessionID); err != nil {
			h.logger.Warnf("failed to remove login session from Redis: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
