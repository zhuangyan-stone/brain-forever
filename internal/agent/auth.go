package agent

import (
	"net/http"
)

// RequireAuth wraps an http.HandlerFunc with anonymous session checking.
// If the session is anonymous (not logged in), returns 401 Unauthorized with empty body.
// The frontend detects the 401 status and handles the prompt itself.
func (h *ChatAgent) RequireAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := h.resolveSessionID(w, r)
		session := h.sessionManager.GetOrCreate(sessionID)
		if session.IsAnonymous() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fn(w, r)
	}
}
