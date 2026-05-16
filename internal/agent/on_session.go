package agent

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// ============================================================
// Session handler — GET /api/session
// ============================================================

// OnRestoreSession handles GET /api/session — returns current session info
func (h *ChatAgent) OnRestoreSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, isNew := h.getSessionID(w, r)

	var history []Message
	title := ""
	titleState := int(TitleStateOriginal)

	if !isNew {
		// Get a snapshot of history (copy) — lock is released inside GetHistory
		var session *session
		history, session = h.sessionManager.GetHistory(sessionID)

		if history == nil || session == nil {
			history = []Message{}
		} else {
			titleState = int(session.GetTitleState())
			if savedTitle := session.GetTitle(); savedTitle != "" {
				title = savedTitle
			} else {
				for _, msg := range history {
					if msg.Role == "user" {
						title = truncateTitle(msg.Content)
						session.SetTitle(title)
						break
					}
				}
			}
		}
	}

	resp := map[string]interface{}{
		"session_id":  sessionID,
		"is_new":      isNew,
		"history":     history,
		"title":       title,
		"title_state": titleState,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// truncateTitle truncates a string to at most 50 characters for use as a session title.
// It also collapses whitespace/newlines into a single space.
func truncateTitle(s string) string {
	// Collapse whitespace and newlines
	runes := []rune(s)
	var result []rune
	space := false
	for _, r := range runes {
		switch r {
		case '\n', '\r', '\t', ' ':
			if !space {
				result = append(result, ' ')
				space = true
			}
		default:
			result = append(result, r)
			space = false
		}
	}
	trimmed := string(result)
	// Limit to 50 characters
	runes2 := []rune(trimmed)
	if len(runes2) > 50 {
		return string(runes2[:50]) + "…"
	}
	return trimmed
}

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

// ============================================================
// PutSessionTitle handler — PUT /api/session/title?title=XXX&state=N
// ============================================================

// OnPutSessionTitle handles PUT /api/session/title — updates the session title
// and marks the title state.
// Query parameters:
//
//	title — the new title to set (required)
//	state — title modification state: 0=original, 1=AI-modified, 2=user-modified (default: 2)
//
// Returns HTTP 200 on success.
func (h *ChatAgent) OnPutSessionTitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the new title from query parameter
	newTitle := r.URL.Query().Get("title")
	if newTitle == "" {
		http.Error(w, "title query parameter is required", http.StatusBadRequest)
		return
	}

	// Read the optional state parameter (default: user-modified)
	stateStr := r.URL.Query().Get("state")
	titleState := TitleStateUserModified // default
	if stateStr != "" {
		if v, err := strconv.Atoi(stateStr); err == nil {
			switch v {
			case 0:
				titleState = TitleStateOriginal
			case 1:
				titleState = TitleStateAIModified
			case 2:
				titleState = TitleStateUserModified
			}
		}
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Update the session title
	session.SetTitle(newTitle)
	// Set the title state as specified
	session.SetTitleState(titleState)

	// Return simple 200 OK
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
