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
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Read the in-memory chat list under chatsMu lock
	session.user.chatsMu.Lock()
	chats := session.user.chats
	session.user.chatsMu.Unlock()

	// If the in-memory list is empty, try loading from DB
	if len(chats) == 0 {
		session.mu.Lock()
		userSN := session.user.SN
		session.mu.Unlock()

		if userSN != "" {
			// Logged-in user: load chats from DB via UserStore.LoadChats
			if loadedChats, err := store.TheUserStore().LoadChats(userSN); err == nil {
				chats = loadedChats
				session.switchToUser(session.user.ID, userSN, chats)
			}
		}

		// Re-read after potential load
		session.user.chatsMu.Lock()
		chats = session.user.chats
		session.user.chatsMu.Unlock()
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
