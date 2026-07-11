package notify

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"BrainForever/infra/captcha"
)

// ============================================================
// Handler provides HTTP handlers for internal notifications.
// ============================================================

// Handler holds dependencies for notify endpoints.
type Handler struct {
	captchaProvider *captcha.CaptchaProvider
}

// NewHandler creates a new notify Handler.
func NewHandler(captchaProvider *captcha.CaptchaProvider) *Handler {
	return &Handler{
		captchaProvider: captchaProvider,
	}
}

// ============================================================
// PUT /api/notify/captcha/refresh — RefreshCaptcha
// ============================================================

// refreshCaptchaRequest is the JSON body for refreshing captcha.
type refreshCaptchaRequest struct {
	Dir string `json:"dir"` // Directory to activate: "d1" or "d2"
}

// OnCaptchaRefresh handles PUT /api/notify/captcha/refresh.
// It refreshes the captcha provider's active directory.
// Only requests from 127.0.0.1 (localhost) are accepted.
func (h *Handler) OnCaptchaRefresh(w http.ResponseWriter, r *http.Request) {
	// Verify the request comes from localhost
	remoteIP := resolveClientIP(r)
	if remoteIP != "127.0.0.1" && remoteIP != "::1" {
		http.Error(w, "forbidden: only localhost allowed", http.StatusForbidden)
		return
	}

	var req refreshCaptchaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate dir
	dir := strings.TrimSpace(req.Dir)
	if dir != "d1" && dir != "d2" {
		http.Error(w, "invalid dir: must be \"d1\" or \"d2\"", http.StatusBadRequest)
		return
	}

	if err := h.captchaProvider.Refresh(r.Context(), dir); err != nil {
		http.Error(w, "failed to refresh captcha: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"dir":    dir,
	})
}

// resolveClientIP extracts the client IP address from the request,
// handling the X-Forwarded-For header and Go's RemoteAddr format.
func resolveClientIP(r *http.Request) string {
	// Try X-Forwarded-For first (for reverse proxy setups)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
