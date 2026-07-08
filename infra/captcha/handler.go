package captcha

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/i18n"
	"BrainForever/internal/session"
)

// ============================================================
// Captcha HTTP handler
// ============================================================

// validActions defines the set of actions that are allowed for captcha.
var validActions = map[string]bool{
	"login":    true,
	"resetpwd": true,
}

// Handler provides HTTP handlers for captcha-related operations.
type Handler struct {
	sessionManager *session.Manager
	cookieName     string
}

// NewHandler creates a new captcha Handler.
func NewHandler(sessionManager *session.Manager, cookieName string) *Handler {
	return &Handler{
		sessionManager: sessionManager,
		cookieName:     cookieName,
	}
}

// captchaCodeRequest is the JSON body for captcha verification.
type captchaCodeRequest struct {
	Code string `json:"code"`
}

// OnGetCaptcha handles GET /api/verify/captcha?action=...
// It generates a captcha and returns the image URL and action.
func (h *Handler) OnGetCaptcha(w http.ResponseWriter, r *http.Request) {
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)
	if sess == nil {
		http.Error(w, i18n.T("api_error_internal"), http.StatusBadRequest)
		return
	}

	action := r.URL.Query().Get("action")
	if action == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "action"}), http.StatusBadRequest)
		return
	}

	if !validActions[action] {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "unsupported action " + action}), http.StatusBadRequest)
		return
	}

	// Try to get a captcha that differs from the current one (if any)
	previousCode := getCaptchaCache(sess, action)
	fn, code := GetOneDifferent(previousCode, 10)
	if fn == "" || code == "" {
		http.Error(w, i18n.T("api_error_internal"), http.StatusInternalServerError)
		return
	}

	// Store the captcha code in the session for later verification
	SetCaptchaCache(sess, action, code)

	// Build response
	type CaptchaResponse struct {
		Src    string `json:"src"`
		Action string `json:"action"`
	}

	type Response struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Captcha CaptchaResponse `json:"captcha"`
	}

	resp := Response{
		Status:  "ok",
		Message: "captcha image generated",
		Captcha: CaptchaResponse{
			Src:    "/static/img/captchas/" + fn + ".jpg",
			Action: action,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// OnVerifyCaptcha handles POST /api/verify/captcha?action=...
// It checks the user-submitted code against the session-stored captcha code.
// On success, the captcha is consumed (removed from session).
func (h *Handler) OnVerifyCaptcha(w http.ResponseWriter, r *http.Request) {
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)
	if sess == nil {
		http.Error(w, i18n.T("api_error_internal"), http.StatusBadRequest)
		return
	}

	var req captchaCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.Code == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "code"}), http.StatusBadRequest)
		return
	}

	action := r.URL.Query().Get("action")
	if action == "" {
		action = "login"
	}

	if !VerifyCaptchaCache(sess, action, req.Code) {
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "captcha verified",
	})
}
