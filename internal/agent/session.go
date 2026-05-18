package agent

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"sync/atomic"
)

// ============================================================
// Session ID generation & resolution
// ============================================================

// sessionAutoIncID provides thread-safe auto-increment for session ID generation
var sessionAutoIncID atomic.Uint64

// generateSessionID generates a sessionID with a random prefix and auto-increment suffix.
// The random prefix (crypto/rand, 16 bytes → 32 hex chars) prevents session enumeration,
// while the auto-increment suffix preserves ordering for debugging.
// Format: sess-<32hex>-<decimal>
func generateSessionID() string {
	id := sessionAutoIncID.Add(1)

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read should never fail on a modern OS;
		// if it does, fall back to a less secure but still usable ID
		return fmt.Sprintf("sess-fallback-%d", id)
	}

	return fmt.Sprintf("sess-%x-%d", b, id)
}

// StartGC starts the background session GC goroutine.
// It delegates to SessionManager.StartGC so that main.go can start GC
// without needing to access the unexported sessionManager field.
func (h *ChatAgent) StartGC(ctx context.Context) {
	h.sessionManager.StartGC(ctx)
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
