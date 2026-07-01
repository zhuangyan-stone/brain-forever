package agent

import (
	"encoding/json"
	"net/http"

	"BrainForever/internal/local/store"
)

// ============================================================
// Chat favorites handler -GET /api/chat/favorites
// ============================================================

// ListFavoriteChats handles GET /api/chat/favorites -returns all
// favorited chat sessions grouped by their custom_tag.
//
// Returns a JSON object where each key is a custom_tag and the value
// is an array of favorited chat summaries (sn, title, custom_tag,
// create_at, update_at) sorted by update_at DESC, create_at DESC.
func (h *ChatAgent) ListFavoriteChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.chatsMu.Lock()
	chatStore := session.chatsStore
	session.chatsMu.Unlock()

	result, err := chatStore.SelectFavoritedChatTitlesGroupByTags()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure we never return nil -return empty object instead
	if result == nil {
		result = make(map[string][]store.FavoritedChatTitleTag)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
