package user

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"os"
	"strings"

	"BrainForever/infra/i18n"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
	"BrainForever/internal/store/dbc"
)

// ============================================================
// SMS verify-code request handler -POST /api/verify/sms
// ============================================================

type SmsVerifyCodeRequest struct {
	Tel     string `json:"tel"`
	Purpose string `json:"purpose"`
}

// OnRequestVerifyCode handles POST /api/verify/sms.
func (h *Handler) OnRequestVerifyCode(w http.ResponseWriter, r *http.Request) {
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

	verifyCode, err := h.smsCodeCache.Generate(context.Background(), req.Purpose, req.Tel)
	if err != nil {
		h.logger.Errorf("failed to generate SMS verify code for %s: %v", req.Tel, err)
		http.Error(w, i18n.T("api_error_sms_send_failed", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	h.logger.Debugf("SMS verify code for %s: %s (valid for 5 minutes)", req.Tel, verifyCode)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": i18n.T("api_error_sms_code_sent"),
	})
}

// ============================================================
// SMS login handler -POST /api/user/login/sms
// ============================================================

type LoginByTelRequest struct {
	Tel        string `json:"tel"`
	VerifyCode string `json:"code"`
}

// OnLoginBySMS handles POST /api/user/login/sms
func (h *Handler) OnLoginBySMS(w http.ResponseWriter, r *http.Request) {
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

	if h.smsCodeCache == nil || !h.smsCodeCache.Verify(context.Background(), cache.SMS4Login, req.Tel, req.VerifyCode) {
		http.Error(w, i18n.T("api_error_sms_code_invalid"), http.StatusUnauthorized)
		return
	}

	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	user, isNew, err := store.TheUserStore().FindOrCreateByTel(req.Tel)
	if err != nil {
		http.Error(w, i18n.T("api_error_login_failed", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

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

	var userSettings store.UserSettings
	if err := userSettings.FromString(user.Settings); err != nil {
		h.logger.Warnf("failed to parse user settings for user %s: %v", user.SN, err)
	}

	sess.SwitchToUser(&session.SessionUser{
		ID:       user.ID,
		SN:       user.SN,
		No:       user.No,
		Nickname: user.Nickname,
		Chats:    chats,
		Settings: userSettings,
	})

	if h.sessionManager.Redis != nil {
		settingsJSON := userSettings.ToString()
		if err := h.sessionManager.Redis.SetLoginSession(
			h.sessionManager.Ctx, sessionID, user.ID, user.SN, settingsJSON,
		); err != nil {
			h.logger.Warnf("failed to persist login session to Redis: %v", err)
		}
	}

	avatar := pickRandomAvatar(h.avatarDir)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"user_sn":  user.SN,
		"no":       user.No,
		"nickname": user.Nickname,
		"avatar":   avatar,
		"chats":    chats,
		"theme":    userSettings.Theme,
		"is_new":   isNew,
	})
}

// ============================================================
// Password login handler -POST /api/user/login/pwd
// ============================================================

type LoginByPwdRequest struct {
	No       string `json:"no"`
	Password string `json:"password"`
}

// OnLoginByPwd handles POST /api/user/login/pwd
func (h *Handler) OnLoginByPwd(w http.ResponseWriter, r *http.Request) {
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

	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	user, err := store.TheUserStore().LoginByPassword(req.No, req.Password)
	if err != nil {
		http.Error(w, i18n.T("api_error_login_failed", map[string]any{"Error": err.Error()}), http.StatusUnauthorized)
		return
	}

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

	var userSettings store.UserSettings
	if err := userSettings.FromString(user.Settings); err != nil {
		h.logger.Warnf("failed to parse user settings for user %s: %v", user.SN, err)
	}

	sess.SwitchToUser(&session.SessionUser{
		ID:       user.ID,
		SN:       user.SN,
		No:       user.No,
		Nickname: user.Nickname,
		Chats:    chats,
		Settings: userSettings,
	})

	if h.sessionManager.Redis != nil {
		settingsJSON := userSettings.ToString()
		if err := h.sessionManager.Redis.SetLoginSession(
			h.sessionManager.Ctx, sessionID, user.ID, user.SN, settingsJSON,
		); err != nil {
			h.logger.Warnf("failed to persist login session to Redis: %v", err)
		}
	}

	avatar := pickRandomAvatar(h.avatarDir)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"user_sn":  user.SN,
		"no":       user.No,
		"nickname": user.Nickname,
		"avatar":   avatar,
		"chats":    chats,
		"theme":    userSettings.Theme,
	})
}

// ============================================================
// Helper: pick a random avatar
// ============================================================

func pickRandomAvatar(avatarDir string) string {
	const avatarURLPrefix = "/static/img/avatar/"
	entries, err := os.ReadDir(avatarDir)
	if err != nil {
		return avatarURLPrefix + "anonymous.png"
	}

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
