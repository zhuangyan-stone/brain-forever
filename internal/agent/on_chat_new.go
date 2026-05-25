package agent

import (
	"encoding/json"
	"net/http"
)

// ============================================================
// NewChat handler — POST /api/chat/new
// ============================================================

// OnNewChat handles POST /api/chat/new — initializes the current chat
// and returns its SN. For new conversations where the current chat has
// not yet been assigned an SN (dbSessionID == 0), this endpoint:
//
//  1. For logged-in users: creates a DB session record (via ensureDBSession),
//     which generates an SN and adds the chat to the in-memory list.
//  2. For anonymous users: no DB persistence, returns an empty SN.
//
// The frontend calls this before sending the first message in a new chat,
// so that subsequent operations have a valid SN to reference.
func (h *ChatAgent) OnNewChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Lock session.mu to safely call ensureDBSession and read fields.
	// SN is read directly from dbChat.SN (store.Chat has the SN field),
	// eliminating the need for a separate chatsMu lock + O(n) traversal of session.chats.
	session.mu.Lock()
	ensureDBSession(session)

	sn := session.currentChat.dbChat.SN
	title := session.currentChat.title
	titleState := int(session.currentChat.titleState)
	session.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sn":          sn,
		"title":       title,
		"title_state": titleState,
	})
}

// ensureDBSession is defined in db.go — it creates a DB session record
// for logged-in users if one doesn't exist yet.
