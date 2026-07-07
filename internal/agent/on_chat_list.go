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
func (h *ChatAgent) OnGetChats(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	sess.User.ChatsMu.Lock()
	chats := sess.User.Chats
	sess.User.ChatsMu.Unlock()

	if len(chats) == 0 {
		sess.Mu.Lock()
		userSN := sess.User.SN
		sess.Mu.Unlock()

		if userSN != "" {
			if loadedChats, err := store.TheUserStore().LoadChats(userSN); err == nil {
				chats = loadedChats
				sess.SwitchToUser(sess.User.ID, userSN, chats, sess.User.Settings)
			}
		}

		sess.User.ChatsMu.Lock()
		chats = sess.User.Chats
		sess.User.ChatsMu.Unlock()
	}

	if chats == nil {
		chats = []store.Chat{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chats": chats,
	})
}
