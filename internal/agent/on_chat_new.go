package agent

import (
	"encoding/json"
	"net/http"

	"BrainForever/internal/agent/llmtypes"
)

// ============================================================
// NewChat handler -PUT /api/chat/new
// ============================================================

// OnNewChat handles PUT /api/chat/new -resets currentChat to a "blank chat"
func (h *ChatAgent) OnNewChat(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	sess.Mu.Lock()

	if sess.User.CurrentChat.DBCHat == nil || sess.User.CurrentChat.DBCHat.SN == "" {
		sess.Mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sn":          "",
			"title":       "",
			"title_state": 0,
		})
		return
	}

	sess.User.CurrentChat = &llmtypes.Chat{}

	sess.Mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sn":          "",
		"title":       "",
		"title_state": 0,
	})
}
