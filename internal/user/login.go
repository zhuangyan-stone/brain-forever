package user

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"BrainForever/infra/captcha"
	"BrainForever/infra/i18n"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
	"BrainForever/internal/store/dbc"

	"github.com/redis/go-redis/v9"
)

// ============================================================
// 图形验证码处理 — GET /api/verify/captcha
// ============================================================

// validActions defines the set of actions that are allowed for captcha.
var validActions = map[string]bool{
	"login":    true,
	"resetpwd": true,
}

// captchaCacheKey 构建验证码在 Redis 中的缓存 key。
func captchaCacheKey(sessionID, action string) string {
	return sessionID + "::captcha::" + action
}

// storeCaptchaItem 将验证码条目存入 Redis 缓存（2 分钟有效期）。
func (h *Handler) storeCaptchaItem(ctx context.Context, sessionID, action string, item *captcha.CaptchaItem) error {
	if h.sessionManager.Redis == nil {
		return nil // 无 Redis 时跳过缓存（仅开发/测试场景）
	}
	client := h.sessionManager.Redis.Client()
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	key := captchaCacheKey(sessionID, action)
	return client.Set(ctx, key, string(data), 2*time.Minute).Err()
}

// getCaptchaItem 从 Redis 缓存中读取验证码条目。
func (h *Handler) getCaptchaItem(ctx context.Context, sessionID, action string) (*captcha.CaptchaItem, error) {
	if h.sessionManager.Redis == nil {
		return nil, redis.Nil
	}
	client := h.sessionManager.Redis.Client()
	key := captchaCacheKey(sessionID, action)
	val, err := client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var item captcha.CaptchaItem
	if err := json.Unmarshal([]byte(val), &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// deleteCaptchaItem 删除 Redis 缓存中的验证码条目（验证成功后消费）。
func (h *Handler) deleteCaptchaItem(ctx context.Context, sessionID, action string) error {
	if h.sessionManager.Redis == nil {
		return nil
	}
	client := h.sessionManager.Redis.Client()
	key := captchaCacheKey(sessionID, action)
	return client.Del(ctx, key).Err()
}

// OnGetVerifyCaptcha handles GET /api/verify/captcha?action=...
// 获取一个随机验证码条目，缓存后返回供前端展示。
func (h *Handler) OnGetVerifyCaptcha(w http.ResponseWriter, r *http.Request) {
	sessionID := session.ResolveSessionID(w, r, h.cookieName)

	action := r.URL.Query().Get("action")
	if action == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "action"}), http.StatusBadRequest)
		return
	}
	if !validActions[action] {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "unsupported action " + action}), http.StatusBadRequest)
		return
	}

	item, err := h.captchaProvider.GetOne(r.Context())
	if err != nil {
		h.logger.Errorf("failed to get captcha: %v", err)
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	// 存入 Redis 缓存供后续验证
	if err := h.storeCaptchaItem(r.Context(), sessionID, action, item); err != nil {
		h.logger.Errorf("failed to cache captcha item: %v", err)
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"src":    item.Image,
		"action": action,
		"q_cn":   item.Data.QCn,
		"q_en":   item.Data.QEn,
	})
}

// ============================================================
// SMS verify-code request handler -POST /api/verify/sms
// ============================================================

// OnGetSMSVerifyCode handles GET /api/verify/sms.
// Parameters (query string): tel, action, img_name, click_x, click_y (required).
// 先验证图形验证码的点击坐标，再发送短信。
func (h *Handler) OnGetSMSVerifyCode(w http.ResponseWriter, r *http.Request) {
	if h.smsCodeCache == nil {
		http.Error(w, i18n.T("api_error_sms_send_failed", map[string]any{"Error": "SMS service unavailable"}), http.StatusServiceUnavailable)
		return
	}

	tel := r.URL.Query().Get("tel")
	action := r.URL.Query().Get("action")
	imgName := r.URL.Query().Get("img_name")
	clickXStr := r.URL.Query().Get("click_x")
	clickYStr := r.URL.Query().Get("click_y")

	if tel == "" || action == "" || imgName == "" || clickXStr == "" || clickYStr == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "tel/action/img_name/click_x/click_y"}), http.StatusBadRequest)
		return
	}

	// 解析点击坐标
	var clickX, clickY int
	if _, err := fmt.Sscanf(clickXStr, "%d", &clickX); err != nil {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "click_x must be integer"}), http.StatusBadRequest)
		return
	}
	if _, err := fmt.Sscanf(clickYStr, "%d", &clickY); err != nil {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "click_y must be integer"}), http.StatusBadRequest)
		return
	}

	sessionID := session.ResolveSessionID(w, r, h.cookieName)

	// 从 Redis 取回缓存的验证码
	item, err := h.getCaptchaItem(r.Context(), sessionID, action)
	if err != nil {
		// 验证码不存在或已过期
		http.Error(w, i18n.T("api_error_captcha_expired"), http.StatusUnauthorized)
		return
	}

	// 验证图片名匹配
	if item.Image != imgName {
		h.logger.Debugf("captcha image mismatch: cached=%q, received=%q", item.Image, imgName)
		http.Error(w, i18n.T("api_error_captcha_wrong"), http.StatusUnauthorized)
		return
	}

	// 验证点击坐标是否在矩形区域内
	d := item.Data
	if clickX < d.Left || clickX > d.Right || clickY < d.Top || clickY > d.Bottom {
		h.logger.Debugf("captcha click position mismatch: click=(%d,%d), rect=[%d,%d,%d,%d]",
			clickX, clickY, d.Left, d.Top, d.Right, d.Bottom)
		http.Error(w, i18n.T("api_error_captcha_wrong"), http.StatusUnauthorized)
		return
	}

	// 验证通过，消费掉该验证码
	if err := h.deleteCaptchaItem(r.Context(), sessionID, action); err != nil {
		h.logger.Warnf("failed to delete consumed captcha: %v", err)
	}

	verifyCode, err := h.smsCodeCache.Generate(context.Background(), action, tel)
	if err != nil {
		h.logger.Errorf("failed to generate SMS verify code for %s: %v", tel, err)
		http.Error(w, i18n.T("api_error_sms_send_failed", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	h.logger.Debugf("SMS verify code for %s: %s (valid for 5 minutes)", tel, verifyCode)

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

	if req.Tel == "" || req.VerifyCode == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "tel/code"}), http.StatusBadRequest)
		return
	}

	if h.smsCodeCache == nil {
		http.Error(w, i18n.T("api_error_sms_send_failed", map[string]any{"Error": "SMS service unavailable"}),
			http.StatusInternalServerError)
		return
	}

	exists, matches := h.smsCodeCache.Verify(context.Background(), cache.SMS4Login, req.Tel, req.VerifyCode)
	if !exists {
		http.Error(w, i18n.T("api_error_sms_code_expired"), http.StatusUnauthorized)
		return
	}
	if !matches {
		http.Error(w, i18n.T("api_error_sms_code_wrong"), http.StatusUnauthorized)
		return
	}

	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	user, isNew, err := store.TheUserStore().FindOrCreateByTel(req.Tel)
	if err != nil {
		http.Error(w, i18n.T("api_error_login_failed", map[string]any{"Error": err.Error()}),
			http.StatusInternalServerError)
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

	if req.No == "" || req.Password == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "no/password"}), http.StatusBadRequest)
		return
	}

	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	user, err := store.TheUserStore().LoginByPassword(req.No, req.Password)
	if err != nil {
		http.Error(w, "", http.StatusUnauthorized)
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
