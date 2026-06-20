package agent

import (
	"BrainForever/internal/local/store"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
)

// ============================================================
// Login handler -POST /api/chat/login
// ============================================================

// LoginRequest is the login request body
type LoginRequest struct {
	UserNo string `json:"user_no"` // Global unique user serial number
}

// OnLogin handles POST /api/chat/login -switches the current session
// to a logged-in user, loading their chat history from the database.
func (h *ChatAgent) OnLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse request: %v", err), http.StatusBadRequest)
		return
	}

	if req.UserNo == "" {
		http.Error(w, "user_no is required", http.StatusBadRequest)
		return
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Switch the session to the logged-in user
	session.switchToUser(req.UserNo)

	// Return the user's session list (read under chatsMu protection)
	session.chatsMu.Lock()
	chats := session.chats
	session.chatsMu.Unlock()

	// �?确保 chats 不为 nil：Go �?nil slice 序列化为 JSON �?null�?
	//   前端 if (data.chats) �?null 时为 false，导�?setSidebarChats 不执行，
	//   侧边栏保留着匿名用户的列表（未清除）�?
	//   �?OnGetChats 中的做法保持一致�?
	if chats == nil {
		chats = []store.Chat{}
	}

	// Randomly pick an avatar from the avatar directory
	avatarIndex := rand.Intn(8) + 1 // 1~8
	avatar := fmt.Sprintf("/static/img/avatar/avatar%d.png", avatarIndex)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"user_no": req.UserNo,
		"avatar":  avatar,
		"chats":   chats,
	})
}
