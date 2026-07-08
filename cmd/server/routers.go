package main

import (
	"encoding/json"
	"net/http"
	"os"

	"BrainForever/infra/captcha"
	"BrainForever/infra/httpx"
	"BrainForever/internal/agent"
	"BrainForever/internal/logger"
	"BrainForever/internal/theme"
	"BrainForever/internal/user"
)

// initRouters registers all API routes on the given server.
func initRouters(srv *httpx.Server, chatHandler *agent.ChatAgent, themeHandler *theme.Handler, userHandler *user.Handler, captchaHandler *captcha.Handler) {

	// ============================================================
	// Authenticated routes (require valid session)
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
	srv.POST("/api/user/logout", chatHandler.RequireAuth(userHandler.OnLogout))

	// /api/user/portrait -- GET (generate user portrait, streaming SSE)
	srv.GET("/api/user/portrait", chatHandler.RequireAuth(chatHandler.OnGetUserPortrait))

	// /api/user/portrait/title -- POST (generate overall title for a document, e.g. portrait)
	srv.POST("/api/user/portrait/title", chatHandler.RequireAuth(chatHandler.OnGetPortraitTitle))

	// ============================================================
	// User theme routes (require auth)
	// ============================================================

	// /api/user/theme/apply -- POST (apply user theme selection, requires auth)
	srv.POST("/api/user/theme/apply", chatHandler.RequireAuth(userHandler.ApplyTheme))

	// /api/user/theme -- GET (get user theme preferences, requires auth)
	srv.GET("/api/user/theme", chatHandler.RequireAuth(userHandler.GetTheme))

	// /api/user/theme/mode -- PUT (update active theme mode: light/dark/system)
	srv.PUT("/api/user/theme/mode", chatHandler.RequireAuth(userHandler.ApplyThemeMode))

	// /api/user/theme/sync -- PUT (update theme sync preference, with optional theme data)
	srv.PUT("/api/user/theme/sync", chatHandler.RequireAuth(userHandler.ApplyThemeSync))

	// /api/themes/mainfes -- GET (read theme manifest, no auth required)
	srv.GET("/api/themes/mainfes", themeHandler.GetThemeMainfes)

	// ============================================================
	// Public routes (no authentication required)
	// ============================================================

	// /api/verify/sms -- GET (request SMS verification code)
	srv.GET("/api/verify/sms", userHandler.OnGetSMSVerifyCode)

	// /api/verify/captcha -- GET (get captcha image); verification is embedded in /api/verify/sms
	srv.GET("/api/verify/captcha", captchaHandler.OnGetVerifyCaptcha)

	// /api/user/login/sms -- POST (login by tel + SMS verify code)
	srv.POST("/api/user/login/sms", userHandler.OnLoginBySMS)

	// /api/user/login/pwd -- POST (login by no + password)
	srv.POST("/api/user/login/pwd", userHandler.OnLoginByPwd)

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

// serveFileOr404 reads and serves a file, returning 404 with a log if not found.
func serveFileOr404(w http.ResponseWriter, r *http.Request, filePath string) {
	f, err := os.Open(filePath)
	if err != nil {
		logger.TheLogger().Errorf("serveFileOr404: failed to open %q: %v", filePath, err)
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		logger.TheLogger().Errorf("serveFileOr404: failed to stat %q: %v", filePath, err)
		http.NotFound(w, r)
		return
	}

	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

// isHomePage returns true if the request path is the main app page ("/" or "/index.html").
func isHomePage(path string) bool {
	return path == "/" || path == "/index.html"
}

// isSigninPage returns true if the request path is the signin page.
// Supports both the new directory-style URL (/signin/) and the old file-style (/signin.html)
// for backward compatibility.
func isSigninPage(path string) bool {
	return path == "/signin/" || path == "/signin/index.html"
}

// redirectSignin sends a 302 Found redirect to the signin page.
// Uses StatusFound (302) instead of StatusMovedPermanently (301) to prevent
// browser caching of the redirect, so logout → re-login works correctly.
func redirectSignin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/signin/", http.StatusFound)
}

// initStaticFileServer sets up the static file server for frontend pages.
// When cacheDisable is true, sets Cache-Control: no-cache headers so frontend changes
// take effect immediately during development.
// Production (default) uses http.FileServer's default ETag/Last-Modified caching behavior.
// HTML pages ("/", "/index.html", "/signin/") always bypass cache regardless of cacheDisable:
//   - Home page: also checked for login → anonymous sessions get 302 to /signin/
//   - Signin page: no auth check, just no-cache to ensure fresh content
//
// NOTE: With Go 1.22+ http.ServeMux, when method-qualified routes (e.g. "GET /api/...")
// are registered alongside a catch-all "/" pattern, http.FileServer may fail to serve
// index.html for subdirectory paths like /signin/. We use http.ServeFile explicitly for
// the signin page to work around this issue.
func initStaticFileServer(srv *httpx.Server, frontendDir string, cacheDisable bool, chatHandler *agent.ChatAgent) {
	fs := http.FileServer(http.Dir(frontendDir))
	srv.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Signin page: use explicit file reading to bypass http.FileServer
		// subdirectory issues with Go 1.22+ ServeMux method-qualified routes,
		// and to avoid path resolution issues with http.ServeFile.
		if isSigninPage(path) {
			setNoCacheHeaders(w)
			serveFileOr404(w, r, frontendDir+"/signin/index.html")
			return
		}

		// Home page: disable cache + check login status
		if isHomePage(path) {
			setNoCacheHeaders(w)

			// Anonymous session -> redirect to signin page
			if chatHandler != nil && chatHandler.IsSessionAnonymous(w, r) {
				redirectSignin(w, r)
				return
			}
		} else if cacheDisable {
			setNoCacheHeaders(w)
		}

		fs.ServeHTTP(w, r)
	})
}
