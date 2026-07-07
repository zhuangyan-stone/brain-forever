package agent

import (
	"encoding/json"
	"net/http"
	"strings"

	"BrainForever/infra/i18n"
	"BrainForever/internal/store"
)

// ============================================================
// Chat favorites handler -GET /api/chat/favorites
// ============================================================

// ListFavoriteChats handles GET /api/chat/favorites
func (h *ChatAgent) ListFavoriteChats(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	chatStore, cerr := h.openChatDB(sess)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	result, err := chatStore.SelectFavoritedChatTitlesGroupByTags()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if result == nil {
		result = make(map[string][]store.FavoritedChatTitleTag)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// AddFavoriteChat handles PUT /api/chat/favorites?sn=xxx&custom_tag=yyy
func (h *ChatAgent) AddFavoriteChat(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}
	customTag := strings.TrimSpace(r.URL.Query().Get("custom_tag"))

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	chatStore, cerr := h.openChatDB(sess)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	sess.User.ChatsMu.Lock()
	var chatID int64
	for _, c := range sess.User.Chats {
		if c.SN == sn {
			chatID = c.ID
			break
		}
	}
	sess.User.ChatsMu.Unlock()

	if chatID == 0 {
		http.Error(w, i18n.T("api_error_chat_not_found"), http.StatusNotFound)
		return
	}

	exists, err := chatStore.IsExistsFavoriteItem(chatID, customTag)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if exists {
		displayTag := customTag
		if displayTag == "" {
			displayTag = i18n.T("favorites_root_directory")
		}
		http.Error(w, i18n.T("favorites_already_in_tag", map[string]any{"Tag": displayTag}), http.StatusConflict)
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
func (h *ChatAgent) RemoveFavoriteChat(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}
	customTag := r.URL.Query().Get("custom_tag")

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	chatStore, cerr := h.openChatDB(sess)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	sess.User.ChatsMu.Lock()
	var chatID int64
	for _, c := range sess.User.Chats {
		if c.SN == sn {
			chatID = c.ID
			break
		}
	}
	sess.User.ChatsMu.Unlock()

	if chatID == 0 {
		http.Error(w, i18n.T("api_error_chat_not_found"), http.StatusNotFound)
		return
	}

	if err := chatStore.DeleteFavoriteItem(chatID, customTag); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
