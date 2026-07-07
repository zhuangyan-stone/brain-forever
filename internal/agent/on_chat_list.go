package agent

import (
	"encoding/json"
	"net/http"

	"BrainForever/internal/session"
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
		userNo := sess.User.No
		userNickname := sess.User.Nickname
		userID := sess.User.ID
		userSettings := sess.User.Settings
		sess.Mu.Unlock()

		if userSN != "" {
			if loadedChats, err := store.TheUserStore().LoadChats(userSN); err == nil {
				chats = loadedChats
				sess.SwitchToUser(session.SessionUser{
					ID:       userID,
					SN:       userSN,
					No:       userNo,
					Nickname: userNickname,
					Chats:    chats,
					Settings: userSettings,
				})
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
