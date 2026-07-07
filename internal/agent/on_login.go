package agent

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"os"
	"strings"

	"BrainForever/infra/i18n"
	"BrainForever/internal/store"
	"BrainForever/internal/store/dbc"
)

// ============================================================
// Login handler -POST /api/user/login
// ============================================================

// LoginRequest is the login request body
type LoginRequest struct {
	No       string `json:"no"`       // User number (6 chars)
	Password string `json:"password"` // Raw password
}

// OnLogin handles POST /api/user/login -authenticates by no+password,
// loads user's chat data, switches the session to the logged-in user,
// and persists the login state to Redis.
func (h *ChatAgent) OnLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.No == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "no"}), http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "password"}), http.StatusBadRequest)
		return
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Authenticate user
	user, err := store.TheUserStore().LoginByPassword(req.No, req.Password)
	if err != nil {
		http.Error(w, i18n.T("api_error_login_failed", map[string]any{"Error": err.Error()}), http.StatusUnauthorized)
		return
	}

	// Load chat list (creates chat DB if first login)
	var chats []store.Chat
	cs, bs, err := dbc.InitUserDB(user.ID, user.SN)
	if err == nil {
		chats, err = cs.ListChats(100)
		dbc.CloseLocalChatDB(cs)
		dbc.CloseLocalBrainDB(bs)
	}
	if err != nil || chats == nil {
		chats = []store.Chat{}
	}

	// Parse user's personal settings (API keys, theme, etc.)
	var userSettings store.UserSettings
	if err := userSettings.FromString(user.Settings); err != nil {
		h.logger.Warnf("failed to parse user settings for user %s: %v", user.SN, err)
	}

	// Switch session to logged-in user
	session.switchToUser(user.ID, user.SN, chats, userSettings)

	// Persist login state to Redis (if available), including user settings
	if h.sessionManager.redis != nil {
		settingsJSON := userSettings.ToString()
		if err := h.sessionManager.redis.SetLoginSession(
			h.sessionManager.ctx, sessionID, user.ID, user.SN, settingsJSON,
		); err != nil {
			// Log the error but don't fail the login — Redis is optional
			h.logger.Warnf("failed to persist login session to Redis: %v", err)
		}
	}

	// Randomly pick an avatar
	avatar := pickRandomAvatar(h.avatarDir)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"user_sn": user.SN,
		"no":      user.No,
		"avatar":  avatar,
		"chats":   chats,
	})
}

// pickRandomAvatar reads the avatar directory, filters files matching avatar*.png,
// and returns a random avatar URL. Falls back to the anonymous avatar if no
// avatar files are found or the directory cannot be read.
func pickRandomAvatar(avatarDir string) string {
	const avatarURLPrefix = "/static/img/avatar/"
	entries, err := os.ReadDir(avatarDir)
	if err != nil {
		return avatarURLPrefix + "anonymous.png"
	}

	// Filter files matching "avatar*.png" (exclude "anonymous.png" and any non-avatar files)
	var avatarFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "avatar") && strings.HasSuffix(name, ".png") {
			avatarFiles = append(avatarFiles, name)
		}
	}

	if len(avatarFiles) == 0 {
		return avatarURLPrefix + "anonymous.png"
	}

	return avatarURLPrefix + avatarFiles[rand.Intn(len(avatarFiles))]
}
