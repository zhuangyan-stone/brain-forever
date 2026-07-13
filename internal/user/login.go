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
	"BrainForever/internal/config"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"

	"github.com/redis/go-redis/v9"
)

// ============================================================
// Image captcha handler — GET /api/verify/captcha
// ============================================================

// validActions defines the set of actions that are allowed for captcha.
var validActions = map[string]bool{
	"login":    true,
	"resetpwd": true,
}

// captchaCacheKey builds the captcha cache key in Redis.
func captchaCacheKey(sessionID, action string) string {
	return sessionID + "::captcha::" + action
}

// storeCaptchaItem stores a captcha item in Redis cache (2-minute TTL).
func (h *Handler) storeCaptchaItem(ctx context.Context, sessionID, action string, item *captcha.CaptchaItem) error {
	if !h.sessionManager.HasRedis() {
		return nil // skip caching when Redis unavailable (dev/test only)
	}
	client := h.sessionManager.Redis().Client()
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	key := captchaCacheKey(sessionID, action)
	return client.Set(ctx, key, string(data), 2*time.Minute).Err()
}

// getCaptchaItem reads a captcha item from Redis cache.
func (h *Handler) getCaptchaItem(ctx context.Context, sessionID, action string) (*captcha.CaptchaItem, error) {
	if !h.sessionManager.HasRedis() {
		return nil, redis.Nil
	}
	client := h.sessionManager.Redis().Client()
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

// deleteCaptchaItem deletes a captcha item from Redis cache (consumed after verification).
func (h *Handler) deleteCaptchaItem(ctx context.Context, sessionID, action string) error {
	if !h.sessionManager.HasRedis() {
		return nil
	}
	client := h.sessionManager.Redis().Client()
	key := captchaCacheKey(sessionID, action)
	return client.Del(ctx, key).Err()
}

// OnGetVerifyCaptcha handles GET /api/verify/captcha?action=...
// Gets a random captcha entry, caches it, and returns it for frontend display.
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

	// Cache in Redis for subsequent verification
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
// Verifies the image captcha click coordinates first, then sends the SMS.
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

	// Parse click coordinates
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

	// Retrieve cached captcha from Redis
	item, err := h.getCaptchaItem(r.Context(), sessionID, action)
	if err != nil {
		// Captcha not found or expired
		http.Error(w, i18n.T("api_error_captcha_expired"), http.StatusUnauthorized)
		return
	}

	// Verify image name match
	if item.Image != imgName {
		h.logger.Debugf("captcha image mismatch: cached=%q, received=%q", item.Image, imgName)
		http.Error(w, i18n.T("api_error_captcha_wrong"), http.StatusUnauthorized)
		return
	}

	// Verify click coordinates are within the rectangular area
	d := item.Data
	if clickX < d.A[0] || clickX > d.A[2] || clickY < d.A[1] || clickY > d.A[3] {
		h.logger.Debugf("captcha click position mismatch: click=(%d,%d), rect=[%d,%d,%d,%d]",
			clickX, clickY, d.A[0], d.A[1], d.A[2], d.A[3])
		http.Error(w, i18n.T("api_error_captcha_wrong"), http.StatusUnauthorized)
		return
	}

	// Verification passed, consume the captcha
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

	h.afterLogin(w, user, isNew, sess)
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

	h.afterLogin(w, user, false, sess)
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

// afterLogin handles common post-login logic: load user chats, parse settings,
// fill system-shared API keys for non-private services, switch session,
// persist to Redis, and respond.
func (h *Handler) afterLogin(w http.ResponseWriter, user *store.User, isNew bool, sess *session.Session) {
	var chats []store.Chat
	cs := store.NewChatStore()
	chats, err := cs.ListChats(user.ID, 100)
	if err != nil || chats == nil {
		chats = []store.Chat{}
	}

	var userSettings store.UserSettings
	if err := userSettings.FromString(user.Settings); err != nil {
		h.logger.Errorf("failed to parse user settings for user %s: %v", user.SN, err)
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	// ============================================================
	// Inject system-shared API keys for non-private services.
	// This fills ApiKey from the global pool (configured in server.toml
	// under [api-keys]) when the user's setting is Private=false.
	//
	// Rules:
	//   - Private=true, key!=""  → keep as-is (user's own key)
	//   - Private=true, key==""  → leave empty (will fail at call time)
	//   - Private=false          → fill from system pool (random pick)
	//
	// IMPORTANT: We only modify the in-memory copy in the session.
	// The Private flag is NOT changed, and nothing is written back to DB.
	// ============================================================
	pool := config.GetApiKeysPool()
	if !userSettings.APIKey.LLM.Private && userSettings.APIKey.LLM.ApiKey == "" {
		if userSettings.APIKey.LLM.Provider == "" {
			userSettings.APIKey.LLM.Provider = config.GetDefaultLLMProvider()
		}
		if k := pool.GetOne("llm", userSettings.APIKey.LLM.Provider); k != "" {
			userSettings.APIKey.LLM.ApiKey = k
		}
	}
	if !userSettings.APIKey.Embedder.Private && userSettings.APIKey.Embedder.ApiKey == "" {
		if userSettings.APIKey.Embedder.Provider == "" {
			userSettings.APIKey.Embedder.Provider = config.GetDefaultEmbeddingProvider()
		}
		if k := pool.GetOne("embedding", userSettings.APIKey.Embedder.Provider); k != "" {
			userSettings.APIKey.Embedder.ApiKey = k
		}
	}
	if !userSettings.APIKey.Search.Private && userSettings.APIKey.Search.ApiKey == "" {
		if userSettings.APIKey.Search.Provider == "" {
			userSettings.APIKey.Search.Provider = config.GetDefaultWebSearchProvider()
		}
		if k := pool.GetOne("websearch", userSettings.APIKey.Search.Provider); k != "" {
			userSettings.APIKey.Search.ApiKey = k
		}
	}

	sess.SwitchToUser(&session.SessionUser{
		ID:       user.ID,
		SN:       user.SN,
		No:       user.No,
		Nickname: user.Nickname,
		Chats:    chats,
		Settings: userSettings,
	})

	if err := h.sessionManager.Redis().SetLoginSession(
		h.sessionManager.Ctx, sess.ID,
		&cache.LoginSessionData{
			UserID:   user.ID,
			UserSN:   user.SN,
			No:       user.No,
			Nickname: user.Nickname,
			Settings: userSettings.ToString(),
		},
	); err != nil {
		h.logger.Errorf("failed to persist login session to Redis: %v", err)
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	avatar := pickRandomAvatar(h.avatarDir)

	resp := map[string]interface{}{
		"status":   "ok",
		"user_sn":  user.SN,
		"no":       user.No,
		"nickname": user.Nickname,
		"avatar":   avatar,
		"chats":    chats,
		"theme":    userSettings.Theme,
		"is_new":   isNew,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
