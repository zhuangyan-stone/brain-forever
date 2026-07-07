package main

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/httpx"
	"BrainForever/internal/agent"
	"BrainForever/internal/theme"
)

// initRouters registers all API routes on the given server.
func initRouters(srv *httpx.Server, chatHandler *agent.ChatAgent, themeHandler *theme.Handler) {

	// ============================================================
	// 需要认证的路由
	// ============================================================

	// /api/chat -- POST (new message) + DELETE (delete chat)
	srv.POST("/api/chat", chatHandler.RequireAuth(chatHandler.OnNewMessage))
	srv.DELETE("/api/chat", chatHandler.RequireAuth(chatHandler.OnChatDelete))

	// Recycle bin (trash) endpoints
	srv.GET("/api/chat/deleted", chatHandler.RequireAuth(chatHandler.OnListDeletedChats))

	// /api/chat/favorites -- GET + PUT + DELETE
	srv.GET("/api/chat/favorites", chatHandler.RequireAuth(chatHandler.ListFavoriteChats))
	srv.PUT("/api/chat/favorites", chatHandler.RequireAuth(chatHandler.AddFavoriteChat))
	srv.DELETE("/api/chat/favorites", chatHandler.RequireAuth(chatHandler.RemoveFavoriteChat))

	// /api/chat/groups -- GET (tag-grouped chat list)
	srv.GET("/api/chat/groups", chatHandler.RequireAuth(chatHandler.OnChatGroups))

	// /api/chat/list -- GET
	srv.GET("/api/chat/list", chatHandler.RequireAuth(chatHandler.OnGetChats))

	// /api/chat/messages -- DELETE
	srv.DELETE("/api/chat/messages", chatHandler.RequireAuth(chatHandler.OnDeleteMessage))

	// /api/chat/new -- PUT
	srv.PUT("/api/chat/new", chatHandler.RequireAuth(chatHandler.OnNewChat))

	// /api/chat/permanent -- DELETE
	srv.DELETE("/api/chat/permanent", chatHandler.RequireAuth(chatHandler.OnPermanentDelete))

	// /api/chat/pin -- PUT
	srv.PUT("/api/chat/pin", chatHandler.RequireAuth(chatHandler.OnChatPin))

	// /api/chat/restore -- PUT
	srv.PUT("/api/chat/restore", chatHandler.RequireAuth(chatHandler.OnRestoreChat))

	// /api/chat/switch -- GET
	srv.GET("/api/chat/switch", chatHandler.RequireAuth(chatHandler.OnSwitchChat))

	// /api/chat/tags -- POST (classify a chat)
	srv.POST("/api/chat/tags", chatHandler.RequireAuth(chatHandler.OnGenerateChatTags))

	// /api/chat/title -- GET (propose title) + PUT (save title)
	srv.GET("/api/chat/title", chatHandler.RequireAuth(chatHandler.OnGetSuggestedChatTitle))
	srv.PUT("/api/chat/title", chatHandler.RequireAuth(chatHandler.OnPutChatTitle))

	// /api/chat/traits -- POST (extract personal traits via LLM directly)
	srv.POST("/api/chat/traits", chatHandler.RequireAuth(chatHandler.OnExtractTraits))

	// /api/chat/trash -- DELETE
	srv.DELETE("/api/chat/trash", chatHandler.RequireAuth(chatHandler.OnEmptyTrash))

	// /api/info/llm/chat -- GET
	srv.GET("/api/info/llm/chat", chatHandler.RequireAuth(chatHandler.OnGetLLMInfo))

	// /api/user/logout -- POST
	srv.POST("/api/user/logout", chatHandler.RequireAuth(chatHandler.OnLogout))

	// /api/user/portrait -- GET (generate user portrait, streaming SSE)
	srv.GET("/api/user/portrait", chatHandler.RequireAuth(chatHandler.OnGetUserPortrait))

	// /api/user/portrait/title -- POST (generate overall title for a document, e.g. portrait)
	srv.POST("/api/user/portrait/title", chatHandler.RequireAuth(chatHandler.OnGetPortraitTitle))

	// /api/themes -- GET (list themes) + POST (update active theme)
	srv.GET("/api/themes", chatHandler.RequireAuth(themeHandler.GetThemes))
	srv.POST("/api/themes", chatHandler.RequireAuth(themeHandler.SetThemes))

	// ============================================================
	// 不需要认证的路由
	// ============================================================

	// /api/user/login -- POST
	srv.POST("/api/user/login", chatHandler.OnLogin)

	// /api/session -- GET
	srv.GET("/api/session", chatHandler.OnSession)

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

// setNoCacheHeaders sets HTTP response headers to disable caching.
func setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// isHomePage returns true if the request path is the main app page ("/" or "/index.html").
func isHomePage(path string) bool {
	return path == "/" || path == "/index.html"
}

// isSigninPage returns true if the request path is the signin page.
func isSigninPage(path string) bool {
	return path == "/signin.html"
}

// redirectSignin sends a 302 Found redirect to the signin page.
// Uses StatusFound (302) instead of StatusMovedPermanently (301) to prevent
// browser caching of the redirect, so logout → re-login works correctly.
func redirectSignin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/signin.html", http.StatusFound)
}

// initStaticFileServer sets up the static file server for frontend pages.
// When cacheDisable is true, sets Cache-Control: no-cache headers so frontend changes
// take effect immediately during development.
// Production (default) uses http.FileServer's default ETag/Last-Modified caching behavior.
// HTML pages ("/", "/index.html", "/signin.html") always bypass cache regardless of cacheDisable:
//   - Home page: also checked for login → anonymous sessions get 302 to /signin.html
//   - Signin page: no auth check, just no-cache to ensure fresh content
func initStaticFileServer(srv *httpx.Server, frontendDir string, cacheDisable bool, chatHandler *agent.ChatAgent) {
	fs := http.FileServer(http.Dir(frontendDir))
	srv.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// HTML 页面对应禁用缓存（不受 cacheDisable 影响）：
		//   - 首页：确保每次访问都经后端登录检查，避免绕过 session 验证
		//   - 登录页：确保页面更新（促销活动、微信扫码等）后用户立即看到
		if isHomePage(path) || isSigninPage(path) {
			setNoCacheHeaders(w)

			// 登录检查：仅对首页生效
			if isHomePage(path) && chatHandler != nil && chatHandler.IsSessionAnonymous(w, r) {
				redirectSignin(w, r)
				return
			}
		} else if cacheDisable {
			setNoCacheHeaders(w)
		}

		fs.ServeHTTP(w, r)
	})
}
