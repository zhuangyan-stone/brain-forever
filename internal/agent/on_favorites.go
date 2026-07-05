package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"BrainForever/internal/store"
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

// AddFavoriteChat handles PUT /api/chat/favorites?sn=xxx&custom_tag=yyy
// to add a chat session to favorites with an optional custom tag.
func (h *ChatAgent) AddFavoriteChat(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}
	customTag := strings.TrimSpace(r.URL.Query().Get("custom_tag"))

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Resolve sn to chat ID
	session.chatsMu.Lock()
	chatStore := session.chatsStore
	var chatID int64
	for _, c := range session.chats {
		if c.SN == sn {
			chatID = c.ID
			break
		}
	}
	session.chatsMu.Unlock()

	if chatID == 0 {
		http.Error(w, "chat not found", http.StatusNotFound)
		return
	}

	// 检查是否已存在相同 (chat_id, custom_tag) 的收藏
	exists, err := chatStore.IsExistsFavoriteItem(chatID, customTag)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if exists {
		displayTag := customTag
		if displayTag == "" {
			displayTag = "根目录"
		}
		http.Error(w, fmt.Sprintf("该对话已在「%s」中收藏", displayTag), http.StatusConflict)
		return
	}

	if err := chatStore.InsertFavoriteItem(chatID, customTag); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// RemoveFavoriteChat handles DELETE /api/chat/favorites?sn=xxx&custom_tag=yyy
// to remove a chat session from favorites. Both sn and custom_tag are required.
func (h *ChatAgent) RemoveFavoriteChat(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}
	customTag := r.URL.Query().Get("custom_tag")

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Resolve sn to chat ID
	session.chatsMu.Lock()
	chatStore := session.chatsStore
	var chatID int64
	for _, c := range session.chats {
		if c.SN == sn {
			chatID = c.ID
			break
		}
	}
	session.chatsMu.Unlock()

	if chatID == 0 {
		http.Error(w, "chat not found", http.StatusNotFound)
		return
	}

	if err := chatStore.DeleteFavoriteItem(chatID, customTag); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
