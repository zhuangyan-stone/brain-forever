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

// IsSessionAnonymous checks whether the HTTP request's session is anonymous
// (not logged in). It resolves the session ID from cookie, gets or creates
// the session, and returns true if the user is not authenticated.
// This is an exported method so it can be called from outside the agent package
// (e.g., from routers.go for static file server auth check).
func (h *ChatAgent) IsSessionAnonymous(w http.ResponseWriter, r *http.Request) bool {
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)
	return session.IsAnonymous()
}
