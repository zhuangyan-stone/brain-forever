package agent

import (
	"encoding/json"
	"net/http"
)

// ============================================================
// NewChat handler -PUT /api/chat/new
// ============================================================

// OnNewChat handles PUT /api/chat/new -resets currentChat to a "blank chat"
// (free pointer) state.
//
// A blank chat has no SN, no DB record, and is NOT in session.user.chats[].
// It represents a fresh new conversation that hasn't sent any messages yet.
// The SN is only generated later when ensureDBSession is called (on first message).
//
// Logic:
//  1. If currentChat is nil or points into session.user.chats[] (a historical chat),
//     reset it to &chat{} (blank, no SN).
//  2. If currentChat is already a blank chat (dbChat == nil or SN == ""),
//     this is a no-op -it's already blank.
//
// Returns JSON: { sn: "", title: "", title_state: 0 }
func (h *ChatAgent) OnNewChat(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.mu.Lock()

	// Check if currentChat is already a blank chat
	if session.user.currentChat.dbChat == nil || session.user.currentChat.dbChat.SN == "" {
		// Already blank -no-op
		session.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sn":          "",
			"title":       "",
			"title_state": 0,
		})
		return
	}

	// currentChat points into session.user.chats[] (a historical chat) -reset to blank
	session.user.currentChat = &chat{}

	session.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sn":          "",
		"title":       "",
		"title_state": 0,
	})
}
