package agent

import (
	"encoding/json"
	"log"
	"net/http"

	"BrainForever/infra/llm"
	"BrainForever/internal/store"
	"BrainForever/toolset"
)

// ============================================================
// Session handler — GET /api/session
// ============================================================

// OnRestoreSession handles GET /api/session — returns current session info
func (h *ChatAgent) OnRestoreSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, isNew := h.getSessionID(w, r)

	var msgs []Message
	title := ""
	titleState := int(TitleStateOriginal)
	userNo := ""
	var chats []store.Chat
	currentChatSN := ""

	if !isNew {
		// Get a snapshot of messages (copy) — lock is released inside GetMessages
		var sess *session
		msgs, sess = h.sessionManager.GetMessages(sessionID)

		if sess == nil {
			msgs = []Message{}
		} else {
			if msgs == nil {
				panic("session's messages is nil")
			}

			savedTitle, savedState := sess.GetTitle()
			if savedTitle != "" {
				title = savedTitle
				titleState = int(savedState)
			} else {
				for _, msg := range msgs {
					if msg.Role == llm.RoleUser {
						title = toolset.TruncateTitle(msg.Content, 50)
						sess.SetTitle(title, TitleStateOriginal)
						break
					}
				}
			}

			// Read the user_no and current chat's dbSessionID from session
			var dbSessionID int64
			sess.mu.Lock()
			userNo = sess.userNo
			if sess.currentChat != nil {
				dbSessionID = sess.currentChat.dbSessionID
			}
			sess.mu.Unlock()

			// If the user is logged in, also build the chat list for the sidebar
			if userNo != "" {
				// IMPORTANT: First sync the current title back to sess.chats.
				// When ensureDBSession adds a new chat via addChatToList,
				// its title is empty (no title exists at creation time).
				// OnRestoreSession then derives/sets a title on currentChat,
				// but this must also be synced to sess.chats so that the
				// copy returned to the frontend has the correct title.
				// Without this, the sidebar shows "新对话" instead of the
				// correct title for newly created chats.
				sess.syncCurrentChatTitleToChatList(title, titleState)

				// Now make a copy of the (synced) chat list to return to the frontend
				sess.chatsMu.Lock()
				if len(sess.chats) > 0 {
					// Deduplicate to guard against any lingering duplicates
					// from the unsafe slice manipulation that was fixed in persistMessageToDB.
					chats = deduplicateChats(sess.chats)
				} else {
					chats = []store.Chat{}
				}
				sess.chatsMu.Unlock()

				// Determine the current session's SN by matching dbSessionID
				if dbSessionID > 0 {
					for _, c := range chats {
						if c.ID == dbSessionID {
							currentChatSN = c.SN
							break
						}
					}
				}
			}
		}
	}

	if !isNew {
		if len(msgs) == 0 && title == "" {
			isNew = true
		}
	}

	resp := map[string]interface{}{
		"session_id":      sessionID,
		"is_new":          isNew,
		"messages":        msgs,
		"title":           title,
		"title_state":     titleState,
		"user_no":         userNo,
		"chats":           chats,
		"current_chat_sn": currentChatSN,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ============================================================
// NewSession handler — POST /api/session/new
// ============================================================

// OnNewSession handles POST /api/session/new — generates a new session ID,
// sets a new cookie, and returns the new session info.
// Only the currentChat is reset; login state (userNo, chats, chatStore)
// is preserved so that logged-in users continue to have their chat list
// and DB persistence for the new session.
func (h *ChatAgent) OnNewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read current session ID from cookie
	sessionID, isNew := h.getSessionID(w, r)

	if !isNew {
		// Preserve login state: only reset currentChat to empty,
		// keep userNo, chats, chatStore intact.
		session := h.sessionManager.GetOrCreate(sessionID)
		session.mu.Lock()
		session.currentChat = &chat{}
		session.mu.Unlock()

		// Refresh the cookie MaxAge to avoid premature expiry
		h.refreshSession(w, sessionID)
	} else {
		// Brand new session (no cookie at all)
		h.sessionManager.GetOrCreate(sessionID)
	}

	resp := map[string]interface{}{
		"session_id": sessionID,
		"is_new":     true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ============================================================
// Session delete handler — DELETE /api/session?sn=XXX
// ============================================================

// OnDeleteSession handles DELETE /api/session — logically deletes a session.
// Uses chatsMu because it operates on session.chats (independent of streaming).
func (h *ChatAgent) OnDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.chatsMu.Lock()
	defer session.chatsMu.Unlock()

	if session.chatStore == nil {
		http.Error(w, "user not logged in", http.StatusBadRequest)
		return
	}

	if err := session.chatStore.LogicDelete(sn); err != nil {
		log.Printf("failed to delete session: %v", err)
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	// Remove from in-memory cache
	filtered := make([]store.Chat, 0, len(session.chats))
	for _, s := range session.chats {
		if s.SN != sn {
			filtered = append(filtered, s)
		}
	}
	session.chats = filtered

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
