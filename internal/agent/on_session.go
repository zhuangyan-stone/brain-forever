package agent

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/i18n"
)

// ============================================================
// Session handler — GET /api/session
// ============================================================

// OnSession handles GET /api/session
// 创建或获取 HTTP session，返回 session 层面的信息（user_no + welcome）。
// 只通过 cookie 识别 http-session-sn，不带 query 参数。
func (h *ChatAgent) OnSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// 根据 defaultLang 获取本地化的欢迎语
	welcome := i18n.TL(h.defaultLang, "welcome_message")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_no": session.userNo,
		"welcome": welcome,
	})
}
