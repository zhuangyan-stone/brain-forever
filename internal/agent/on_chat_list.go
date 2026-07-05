package agent

import (
	"encoding/json"
	"net/http"

	"BrainForever/internal/store"
)

// ============================================================
// Chat list handler -GET /api/chat/list
// ============================================================

// OnGetChats handles GET /api/chat/list -returns the chat list
// for the current HTTP session's user (including anonymous users).
//
// The user is identified by the HTTP session cookie, not by a query parameter.
// Returns only chat metadata (id, sn, title, title_state, pinned,
// taged, create_at, update_at, etc.) without messages or web sources.
// Messages are loaded lazily when the user switches to a specific chat.
func (h *ChatAgent) OnGetChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Read the in-memory chat list under chatsMu lock
	session.chatsMu.Lock()
	chats := session.chats
	session.chatsMu.Unlock()

	// If the in-memory list is empty, try loading from DB
	if len(chats) == 0 {
		session.mu.Lock()
		userNo := session.userNo
		session.mu.Unlock()

		if userNo != "" {
			// Logged-in user: switchToUser loads chats from DB
			session.switchToUser(userNo)
		} else {
			// Anonymous user: load from anonymous store
			session.chatsMu.Lock()
			loadedChats, err := session.chatsStore.ListChats(100)
			if err == nil {
				session.chats = loadedChats
				chats = loadedChats
			}
			session.chatsMu.Unlock()
		}

		// Re-read after potential load
		session.chatsMu.Lock()
		chats = session.chats
		session.chatsMu.Unlock()
	}

	// Ensure we never return nil -return empty array instead
	if chats == nil {
		chats = []store.Chat{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chats": chats,
	})
}
