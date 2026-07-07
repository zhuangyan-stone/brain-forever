package agent

import (
	"net/http"
)

// RequireAuth wraps an http.HandlerFunc with anonymous session checking.
func (h *ChatAgent) RequireAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := h.resolveSessionID(w, r)
		sess := h.sessionManager.GetOrCreate(sessionID)
		if sess.IsAnonymous() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fn(w, r)
	}
}

// IsSessionAnonymous checks whether the HTTP request's session is anonymous.
func (h *ChatAgent) IsSessionAnonymous(w http.ResponseWriter, r *http.Request) bool {
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)
	return sess.IsAnonymous()
}
