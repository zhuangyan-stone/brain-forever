package user

import (
	"encoding/json"
	"net/http"

	"BrainForever/internal/session"
	"BrainForever/internal/store"
)

// ============================================================
// Logout handler -POST /api/user/logout
// ============================================================

// OnLogout handles POST /api/user/logout -clears the current session's
// user state, returning it to an unauthenticated state.
func (h *Handler) OnLogout(w http.ResponseWriter, r *http.Request) {
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	// Record the user SN before clearing the session state.
	userSN := sess.User.SN

	sess.SwitchToUser(&session.SessionUser{})

	if userSN != "" {
		store.TheUserStore().Logout(userSN)
	}

	if err := h.sessionManager.Redis().DelLoginSession(h.sessionManager.Ctx, sessionID); err != nil {
		h.logger.Warnf("failed to remove login session from Redis. %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
