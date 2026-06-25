package agent

import (
	"BrainForever/internal/local/store"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
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

	// Ensure chats is not nil: Go's nil slice serializes to JSON null.
	//   The frontend's `if (data.chats)` evaluates to false when null,
	//   causing setSidebarChats not to execute and leaving the anonymous
	//   user's chat list in the sidebar (never cleared).
	//   This is consistent with the approach in OnGetChats.
	if chats == nil {
		chats = []store.Chat{}
	}

	// Randomly pick an avatar from the avatar directory
	avatar := pickRandomAvatar(h.avatarDir)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"user_no": req.UserNo,
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
