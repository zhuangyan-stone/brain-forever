package agent

import (
	"BrainForever/toolset"
	"net/http"
	"sync/atomic"
)

// ============================================================
// Session ID generation & resolution
// ============================================================

// sessionAutoIncID provides thread-safe auto-increment for session ID generation
var sessionAutoIncID atomic.Uint64

// generateSessionID generates a locally unique HTTP session ID.
// Only needs local uniqueness (single-server scope), so uses the lightweight
// GenerateSNSimple (UUID v4) rather than the three-factor GenerateSN.
// Format: s-xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
func generateSessionID() string {
	return toolset.GenerateSNSimple("s")
}

// getSessionID gets the sessionID from the request
// Prefers reading from cookie; if absent, generates a new UUID and writes it to cookie
// Returns the sessionID and a bool indicating whether this is a newly created session
func (h *ChatAgent) getSessionID(w http.ResponseWriter, r *http.Request) (string, bool) {
	// Try to read from cookie
	cookie, err := r.Cookie(h.cookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value, false
	}

	// No cookie, generate a new sessionID
	sessionID := generateSessionID()

	// Write cookie (HttpOnly prevents XSS access, Path=/ makes it effective for all paths)
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // Expires in 7 days
	})

	return sessionID, true
}

// refreshSession writes the given sessionID into the cookie with a fresh MaxAge,
// effectively refreshing the session cookie expiry without generating a new ID.
func (h *ChatAgent) refreshSession(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // Expires in 7 days
	})
}

// resolveSessionID is a convenience wrapper around getSessionID that discards the isNew flag.
// Use this when you only need the sessionID string and don't care whether it's new.
func (h *ChatAgent) resolveSessionID(w http.ResponseWriter, r *http.Request) string {
	sessionID, _ := h.getSessionID(w, r)
	return sessionID
}
