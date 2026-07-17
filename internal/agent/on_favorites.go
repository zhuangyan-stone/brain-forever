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

	result, err := theChatStore.SelectFavoritedChatTitlesGroupByTags(sess.User.ID)
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

	exists, err := theChatStore.IsExistsFavoriteItem(chatID, customTag)
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

	if err := theChatStore.InsertFavoriteItem(chatID, customTag); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// RenameFavoriteChatRequest is the JSON body for renaming a favorite item.
type RenameFavoriteChatRequest struct {
	ID           int64  `json:"id"`
	NewCustomTag string `json:"new_custom_tag"`
}

// RenameFavoriteChat handles POST /api/chat/favorites/rename
func (h *ChatAgent) RenameFavoriteChat(w http.ResponseWriter, r *http.Request) {
	var req RenameFavoriteChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.ID == 0 {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "id"}), http.StatusBadRequest)
		return
	}

	if err := theChatStore.RenameFavoriteItem(req.ID, req.NewCustomTag); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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

	if err := theChatStore.DeleteFavoriteItem(chatID, customTag); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// RenameFavoriteCategoryRequest is the JSON body for renaming a favorite category.
type RenameFavoriteCategoryRequest struct {
	OldCustomTag string `json:"old_custom_tag"`
	NewCustomTag string `json:"new_custom_tag"`
}

// RenameFavoriteCategory handles POST /api/chat/favorites/category/rename
// Renames all items in a favorite category from old_custom_tag to new_custom_tag.
func (h *ChatAgent) RenameFavoriteCategory(w http.ResponseWriter, r *http.Request) {
	var req RenameFavoriteCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.OldCustomTag == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "old_custom_tag"}), http.StatusBadRequest)
		return
	}
	if req.NewCustomTag == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "new_custom_tag"}), http.StatusBadRequest)
		return
	}

	rows, err := theChatStore.UpdateFavoriteItemsCustomTag(req.OldCustomTag, req.NewCustomTag)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "rows_affected": rows})
}

// ClearFavoriteCategoryRequest is the JSON body for clearing a favorite category.
type ClearFavoriteCategoryRequest struct {
	CustomTag string `json:"custom_tag"`
}

// ClearFavoriteCategory handles DELETE /api/chat/favorites/category
// Deletes all favorite items in the specified category.
func (h *ChatAgent) ClearFavoriteCategory(w http.ResponseWriter, r *http.Request) {
	var req ClearFavoriteCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.CustomTag == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "custom_tag"}), http.StatusBadRequest)
		return
	}

	rows, err := theChatStore.DeleteFavoriteItemsByCustomTag(req.CustomTag)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "rows_affected": rows})
}
