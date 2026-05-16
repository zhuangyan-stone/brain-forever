package agent

import (
	"encoding/json"
	"net/http"
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
