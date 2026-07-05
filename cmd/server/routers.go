package main

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/httpx"
	"BrainForever/internal/agent"
)

// initRouters registers all API routes on the given server.
func initRouters(srv *httpx.Server, chatHandler *agent.ChatAgent) {

	// /api/chat -- POST (new message) + DELETE (delete chat)
	srv.POST("/api/chat", chatHandler.OnNewMessage)
	srv.DELETE("/api/chat", chatHandler.OnChatDelete)

	// Recycle bin (trash) endpoints
	srv.GET("/api/chat/deleted", chatHandler.OnListDeletedChats)

	// /api/chat/favorites -- GET + PUT + DELETE
	srv.GET("/api/chat/favorites", chatHandler.ListFavoriteChats)
	srv.PUT("/api/chat/favorites", chatHandler.AddFavoriteChat)
	srv.DELETE("/api/chat/favorites", chatHandler.RemoveFavoriteChat)

	// /api/chat/groups -- GET (tag-grouped chat list)
	srv.GET("/api/chat/groups", chatHandler.OnChatGroups)

	// /api/chat/list -- GET
	srv.GET("/api/chat/list", chatHandler.OnGetChats)

	// /api/chat/messages -- DELETE
	srv.DELETE("/api/chat/messages", chatHandler.OnDeleteMessage)

	// /api/chat/new -- PUT
	srv.PUT("/api/chat/new", chatHandler.OnNewChat)

	// /api/chat/permanent -- DELETE
	srv.DELETE("/api/chat/permanent", chatHandler.OnPermanentDelete)

	// /api/chat/pin -- PUT
	srv.PUT("/api/chat/pin", chatHandler.OnChatPin)

	// /api/chat/restore -- PUT
	srv.PUT("/api/chat/restore", chatHandler.OnRestoreChat)

	// /api/chat/switch -- GET
	srv.GET("/api/chat/switch", chatHandler.OnSwitchChat)

	// /api/chat/tags -- POST (classify a chat)
	srv.POST("/api/chat/tags", chatHandler.OnGenerateChatTags)

	// /api/chat/title -- GET (propose title) + PUT (save title)
	srv.GET("/api/chat/title", chatHandler.OnGetSuggestedChatTitle)
	srv.PUT("/api/chat/title", chatHandler.OnPutChatTitle)

	// /api/chat/traits -- POST (extract personal traits via LLM directly)
	srv.POST("/api/chat/traits", chatHandler.OnExtractTraits)

	// /api/chat/trash -- DELETE
	srv.DELETE("/api/chat/trash", chatHandler.OnEmptyTrash)

	// /api/info/llm/chat -- GET
	srv.GET("/api/info/llm/chat", chatHandler.OnGetLLMInfo)

	// /api/session -- GET
	srv.GET("/api/session", chatHandler.OnSession)

	// /api/user/login -- POST
	srv.POST("/api/user/login", chatHandler.OnLogin)

	// /api/user/logout -- POST
	srv.POST("/api/user/logout", chatHandler.OnLogout)

	// /api/user/portrait -- GET (generate user portrait, streaming SSE)
	srv.GET("/api/user/portrait", chatHandler.OnGetUserPortrait)

	// /api/user/portrait/title -- POST (generate overall title for a document, e.g. portrait)
	srv.POST("/api/user/portrait/title", chatHandler.OnGetDocTitle)

	// Health check endpoint
	srv.GET("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"server":  "local-server",
			"version": "1.0.0",
		})
	})
}
