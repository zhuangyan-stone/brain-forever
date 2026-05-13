package agent

import (
	"encoding/json"
	"net/http"
)

// ============================================================
// NewSession handler — POST /api/session/new
// ============================================================

// OnNewSession handles POST /api/session/new — generates a new session ID,
// sets a new cookie, and returns the new session info.
// The old session's history is effectively abandoned (GC will clean it up later).
func (h *ChatAgent) OnNewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Generate a new session ID and write it to cookie
	// getSessionID with a fresh cookie forces a new session
	sessionID, isNew := h.getSessionID(w, r)

	// If the session already existed (cookie was present), we still need to
	// force-generate a new one. Overwrite the cookie with a brand new session ID.
	if !isNew {
		// Force a new session by generating a fresh ID and setting a new cookie
		sessionID = generateSessionID()
		http.SetCookie(w, &http.Cookie{
			Name:     h.cookieName,
			Value:    sessionID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 7, // Expires in 7 days
		})
	}

	// Create a new empty session in the session manager
	h.sessionManager.GetOrCreate(sessionID)

	resp := map[string]interface{}{
		"session_id": sessionID,
		"is_new":     true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
