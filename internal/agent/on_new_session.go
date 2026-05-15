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
// The old session is immediately cleaned up from the session manager
// to avoid holding abandoned session data in memory for days.
func (h *ChatAgent) OnNewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read current session ID from cookie
	sessionID, isNew := h.getSessionID(w, r)

	// If a session already existed, clean it up immediately and refresh the cookie
	if !isNew {
		h.sessionManager.Remove(sessionID)

		// Refresh the cookie MaxAge to avoid premature expiry
		h.refreshSession(w, sessionID)
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
