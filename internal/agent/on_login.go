package agent

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"os"
	"strings"

	"BrainForever/infra/i18n"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
	"BrainForever/internal/store/dbc"
)

// ============================================================
// SMS verify-code request handler -POST /api/verify/sms
// ============================================================

// SmsVerifyCodeRequest is the request body for requesting an SMS verification code.
type SmsVerifyCodeRequest struct {
	Tel     string `json:"tel"`     // Phone number, e.g. "13800138000"
	Purpose string `json:"purpose"` // Purpose: "login", "regist", "pwdreset", etc.
}

// OnRequestVerifyCode handles POST /api/verify/sms.
// Generates a 6-digit verification code for the given purpose and stores it in Redis.
// The frontend specifies the purpose ("login", "regist", "pwdreset", etc.) so that
// different use cases have isolated codes that don't overwrite each other.
// In production, this would send the code via an SMS provider (e.g., Aliyun SMS, Twilio).
// For development, the code is logged to the server console.
func (h *ChatAgent) OnRequestVerifyCode(w http.ResponseWriter, r *http.Request) {
	if h.smsCodeCache == nil {
		http.Error(w, i18n.T("api_error_sms_send_failed", map[string]any{"Error": "SMS service unavailable"}), http.StatusServiceUnavailable)
		return
	}

	var req SmsVerifyCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.Tel == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "tel"}), http.StatusBadRequest)
		return
	}
	if req.Purpose == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "purpose"}), http.StatusBadRequest)
		return
	}

	// Generate and store verification code for the given purpose
	verifyCode, err := h.smsCodeCache.Generate(context.Background(), req.Purpose, req.Tel)
	if err != nil {
		h.logger.Errorf("failed to generate SMS verify code for %s: %v", req.Tel, err)
		http.Error(w, i18n.T("api_error_sms_send_failed", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	// In production, send the code via SMS provider here.
	// For development, log it so developers can see the code.
	h.logger.Infof("📱 SMS verify code for %s: %s (valid for 5 minutes)", req.Tel, verifyCode)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": i18n.T("api_error_sms_code_sent"),
	})
}

// ============================================================
// SMS login handler -POST /api/user/login/sms
// ============================================================

// LoginByTelRequest is the login request body for phone+SMS-verify-code login.
type LoginByTelRequest struct {
	Tel        string `json:"tel"`  // Phone number
	VerifyCode string `json:"code"` // SMS verification code (field name kept as "code" for frontend compatibility)
}

// OnLoginBySMS handles POST /api/user/login/sms -authenticates by tel+SMS verify code,
// auto-registers if the phone number is new, loads user's chat data,
// switches the session to the logged-in user, and persists the login state to Redis.
func (h *ChatAgent) OnLoginBySMS(w http.ResponseWriter, r *http.Request) {
	var req LoginByTelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.Tel == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "tel"}), http.StatusBadRequest)
		return
	}
	if req.VerifyCode == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "code"}), http.StatusBadRequest)
		return
	}

	// Verify SMS verify code (purpose is always "login" for login requests)
	if h.smsCodeCache == nil || !h.smsCodeCache.Verify(context.Background(), cache.SMS4Login, req.Tel, req.VerifyCode) {
		http.Error(w, i18n.T("api_error_sms_code_invalid"), http.StatusUnauthorized)
		return
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Find or auto-register user by phone number
	user, isNew, err := store.TheUserStore().FindOrCreateByTel(req.Tel)
	if err != nil {
		http.Error(w, i18n.T("api_error_login_failed", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
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
		"is_new":  isNew,
	})
}

// ============================================================
// Password login handler -POST /api/user/login/pwd
// ============================================================

// LoginByPwdRequest is the login request body for no+password login.
type LoginByPwdRequest struct {
	No       string `json:"no"`       // User number (6 chars)
	Password string `json:"password"` // Raw password
}

// OnLoginByPwd handles POST /api/user/login/pwd -authenticates by no+password,
// loads user's chat data, switches the session to the logged-in user,
// and persists the login state to Redis.
func (h *ChatAgent) OnLoginByPwd(w http.ResponseWriter, r *http.Request) {
	var req LoginByPwdRequest
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

	// Authenticate user by no + password
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
