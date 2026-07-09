package user

import (
	"BrainForever/infra/captcha"
	"BrainForever/infra/zylog"
	"BrainForever/internal/session"
	"BrainForever/internal/store/cache"
)

// Handler provides HTTP handlers for user-specific operations.
type Handler struct {
	sessionManager  *session.Manager
	cookieName      string
	logger          zylog.Logger
	avatarDir       string
	smsCodeCache    *cache.SMSCodeCache // nil if Redis not configured
	captchaProvider *captcha.CaptchaProvider
}

// NewHandler creates a new user Handler.
func NewHandler(
	sessionManager *session.Manager,
	cookieName string,
	logger zylog.Logger,
	avatarDir string,
	smsCodeCache *cache.SMSCodeCache,
	captchaProvider *captcha.CaptchaProvider,
) *Handler {
	return &Handler{
		sessionManager:  sessionManager,
		cookieName:      cookieName,
		logger:          logger,
		avatarDir:       avatarDir,
		smsCodeCache:    smsCodeCache,
		captchaProvider: captchaProvider,
	}
}
