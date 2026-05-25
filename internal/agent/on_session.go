package agent

import (
	"encoding/json"
	"log"
	"net/http"

	"BrainForever/infra/llm"
	"BrainForever/internal/store"
	"BrainForever/toolset"
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
	userNo := ""
	var chats []store.Chat

	if !isNew {
		// Get a snapshot of history (copy) — lock is released inside GetHistory
		history, sess := h.sessionManager.GetHistory(sessionID)

		if history == nil || sess == nil {
			history = []Message{}
		} else {
			savedTitle, savedState := sess.GetTitle()
			if savedTitle != "" {
				title = savedTitle
				titleState = int(savedState)
			} else {
				for _, msg := range history {
					if msg.Role == llm.RoleUser {
						title = toolset.TruncateTitle(msg.Content, 50)
						sess.SetTitle(title, TitleStateOriginal)
						break
					}
				}
			}

			// Read the user_no from session (empty string if not logged in)
			sess.mu.Lock()
			userNo = sess.userNo
			sess.mu.Unlock()

			// If the user is logged in, also read the session list for the sidebar
			if userNo != "" {
				sess.chatsMu.Lock()
				// Make a copy to avoid data races
				if len(sess.chats) > 0 {
					chats = make([]store.Chat, len(sess.chats))
					copy(chats, sess.chats)
				} else {
					chats = []store.Chat{}
				}
				sess.chatsMu.Unlock()
			}
		}
	}

	resp := map[string]interface{}{
		"session_id":  sessionID,
		"is_new":      isNew,
		"history":     history,
		"title":       title,
		"title_state": titleState,
		"user_no":     userNo,
		"chats":       chats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
// Session delete handler — DELETE /api/session?sn=XXX
// ============================================================

// OnDeleteSession handles DELETE /api/session — logically deletes a session.
// Uses chatsMu because it operates on session.chats (independent of streaming).
func (h *ChatAgent) OnDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.chatsMu.Lock()
	defer session.chatsMu.Unlock()

	if session.chatStore == nil {
		http.Error(w, "user not logged in", http.StatusBadRequest)
		return
	}

	if err := session.chatStore.LogicDelete(sn); err != nil {
		log.Printf("failed to delete session: %v", err)
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	// Remove from in-memory cache
	filtered := make([]store.Chat, 0, len(session.chats))
	for _, s := range session.chats {
		if s.SN != sn {
			filtered = append(filtered, s)
		}
	}
	session.chats = filtered

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
