package local

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/httpx"
	"BrainForever/internal/agent"
)

// InitRouters registers all API routes on the given server.
func InitRouters(srv *httpx.Server, chatHandler *agent.ChatAgent) {
	srv.GET("/api/info/llm/chat", chatHandler.OnGetLLMInfo)
	srv.GET("/api/chat/favorites", chatHandler.ListFavoriteChats)
	srv.PUT("/api/chat/favorites", chatHandler.AddFavoriteChat)
	srv.DELETE("/api/chat/favorites", chatHandler.RemoveFavoriteChat)
	srv.GET("/api/session", chatHandler.OnSession)
	srv.GET("/api/chat/list", chatHandler.OnGetChats)
	srv.PUT("/api/chat/new", chatHandler.OnNewChat)
	srv.PUT("/api/chat/pin", chatHandler.OnChatPin)
	srv.GET("/api/chat/switch", chatHandler.OnSwitchChat)
	srv.DELETE("/api/chat/messages", chatHandler.OnDeleteMessage)
	srv.POST("/api/chat/login", chatHandler.OnLogin)
	srv.POST("/api/chat/logout", chatHandler.OnLogout)

	// Recycle bin (trash) endpoints
	srv.GET("/api/chat/deleted", chatHandler.OnListDeletedChats)
	srv.PUT("/api/chat/restore", chatHandler.OnRestoreChat)
	srv.DELETE("/api/chat/permanent", chatHandler.OnPermanentDelete)
	srv.DELETE("/api/chat/trash", chatHandler.OnEmptyTrash)

	// Health check endpoint
	srv.GET("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"server":  "local-server",
			"version": "1.0.0",
		})
	})

	// /api/chat —POST (new message) + DELETE (delete chat)
	srv.POST("/api/chat", chatHandler.OnNewMessage)
	srv.DELETE("/api/chat", chatHandler.OnChatDelete)

	// /api/chat/title —GET (propose title) + PUT (save title)
	srv.GET("/api/chat/title", chatHandler.OnGetSuggestedChatTitle)
	srv.PUT("/api/chat/title", chatHandler.OnPutChatTitle)

	// /api/chat/tags —POST (classify a chat)
	srv.POST("/api/chat/tags", chatHandler.OnGenerateChatTags)
	// /api/chat/groups —GET (tag-grouped chat list)
	srv.GET("/api/chat/groups", chatHandler.OnChatGroups)

	// /api/chat/traits —POST (extract personal traits via LLM directly)
	srv.POST("/api/chat/traits", chatHandler.OnExtractTraits)

	// /api/user/portrait —GET (generate user portrait, streaming SSE)
	srv.GET("/api/user/portrait", chatHandler.OnGetUserPortrait)

	// /api/doc/title —POST (generate overall title for a document, e.g. portrait)
	srv.POST("/api/doc/title", chatHandler.OnGetDocTitle)

}
