package agent

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
)

// ============================================================
// Session ID generation & resolution
// ============================================================

// sessionAutoIncID provides thread-safe auto-increment for session ID generation
var sessionAutoIncID atomic.Uint64

// generateSessionID generates a simple auto-increment sessionID
// Uses atomic.Uint64 for thread-safe increment
func generateSessionID() string {
	id := sessionAutoIncID.Add(1)

	// Use auto-increment ID + fixed prefix, simple and reliable
	// For production, consider using crypto/rand for more secure IDs
	return fmt.Sprintf("sess-%d-%x", id, id)
}

// StartGC starts the background session GC goroutine.
// It delegates to SessionManager.StartGC so that main.go can start GC
// without needing to access the unexported sessionManager field.
func (h *ChatHandler) StartGC(ctx context.Context) {
	h.sessionManager.StartGC(ctx)
}

// getSessionID gets the sessionID from the request
// Prefers reading from cookie; if absent, generates a new UUID and writes it to cookie
// Returns the sessionID and a bool indicating whether this is a newly created session
func (h *ChatHandler) getSessionID(w http.ResponseWriter, r *http.Request) (string, bool) {
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

// resolveSessionID is a convenience wrapper around getSessionID that discards the isNew flag.
// Use this when you only need the sessionID string and don't care whether it's new.
func (h *ChatHandler) resolveSessionID(w http.ResponseWriter, r *http.Request) string {
	sessionID, _ := h.getSessionID(w, r)
	return sessionID
}
