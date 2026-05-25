package agent

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"BrainForever/infra/i18n"
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

	var history []Message
	title := ""
	titleState := int(TitleStateOriginal)

	if !isNew {
		// Get a snapshot of history (copy) — lock is released inside GetHistory
		var session *session
		history, session = h.sessionManager.GetHistory(sessionID)

		if history == nil || session == nil {
			history = []Message{}
		} else {
			titleState = int(session.GetTitleState())
			if savedTitle := session.GetTitle(); savedTitle != "" {
				title = savedTitle
			} else {
				for _, msg := range history {
					if msg.Role == llm.RoleUser {
						title = toolset.TruncateTitle(msg.Content, 50)
						session.SetTitle(title)
						break
					}
				}
			}
		}
	}

	resp := map[string]interface{}{
		"session_id":  sessionID,
		"is_new":      isNew,
		"history":     history,
		"title":       title,
		"title_state": titleState,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ============================================================
// NewSession handler — POST /api/session/new
// ============================================================

// OnNewSession handles POST /api/session/new — generates a new session ID,
// sets a new cookie, and returns the new session info.
// The old session is immediately cleaned up from the session manager
// to avoid holding abandoned session data in memory for days.
func (h *ChatAgent) OnNewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read current session ID from cookie
	sessionID, isNew := h.getSessionID(w, r)

	// If a session already existed, clean it up immediately and refresh the cookie
	if !isNew {
		h.sessionManager.Remove(sessionID)

		// Refresh the cookie MaxAge to avoid premature expiry
		h.refreshSession(w, sessionID)
	}

	// Create a new empty session in the session manager
	h.sessionManager.GetOrCreate(sessionID)

	resp := map[string]interface{}{
		"session_id": sessionID,
		"is_new":     true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ============================================================
// PutSessionTitle handler — PUT /api/session/title?title=XXX&state=N
// ============================================================

// OnPutSessionTitle handles PUT /api/session/title — updates the session title
// and marks the title state.
// Query parameters:
//
//	title — the new title to set (required)
//	state — title modification state: 0=original, 1=AI-modified, 2=user-modified (default: 2)
//
// Returns HTTP 200 on success.
func (h *ChatAgent) OnPutSessionTitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the new title from query parameter
	newTitle := r.URL.Query().Get("title")
	if newTitle == "" {
		http.Error(w, "title query parameter is required", http.StatusBadRequest)
		return
	}

	// Read the optional state parameter (default: user-modified)
	stateStr := r.URL.Query().Get("state")
	titleState := TitleStateUserModified // default
	if stateStr != "" {
		if v, err := strconv.Atoi(stateStr); err == nil {
			switch v {
			case 0:
				titleState = TitleStateOriginal
			case 1:
				titleState = TitleStateAIModified
			case 2:
				titleState = TitleStateUserModified
			}
		}
	}

	// Read optional sn parameter — if provided, update a specific session from the list
	// instead of the current active session
	sn := r.URL.Query().Get("sn")

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.mu.Lock()
	defer session.mu.Unlock()

	if sn != "" {
		// Update a specific session from the user's session list (e.g., rename from sidebar)
		if session.chatStore == nil {
			http.Error(w, "user not logged in", http.StatusBadRequest)
			return
		}
		// Find the session by SN
		var targetSession *store.Session
		for i := range session.chats {
			if session.chats[i].SN == sn {
				targetSession = &session.chats[i]
				break
			}
		}
		if targetSession == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		if err := session.chatStore.UpdateSessionTitle(
			targetSession.ID,
			newTitle,
			int8(titleState),
		); err != nil {
			log.Printf("failed to update session title in DB: %v", err)
			http.Error(w, "failed to update session title", http.StatusInternalServerError)
			return
		}
		// Update in-memory cache
		targetSession.Title = newTitle
		targetSession.TitleState = int8(titleState)
	} else {
		// Update the current active session title
		session.SetTitle(newTitle)
		session.SetTitleState(titleState)

		// Sync title to DB if the user is logged in and has a DB session
		if session.chatStore != nil && session.currentChat != nil && session.currentChat.dbSessionID != 0 {
			if err := session.chatStore.UpdateSessionTitle(
				session.currentChat.dbSessionID,
				newTitle,
				int8(titleState),
			); err != nil {
				log.Printf("failed to update session title in DB: %v", err)
			}
		}
	}

	// Return simple 200 OK
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// Session title generation handler — GET /api/session/title?title=XXX
// ============================================================

func extractMessagesForTitle(history []Message) []Message {
	c := len(history)
	if c <= 50 {
		return history
	}

	i := c/3 + 1
	samples := history[0:i]

	for j := i + 1; j < c-1; j++ {
		if history[j].Role == llm.RoleUser {
			samples = append(samples, history[j])
		} else if j%5 == 0 {
			samples = append(samples, history[j])
		}
	}

	samples = append(samples, history[c-1])
	return samples
}

// OnGetSessionTitle handles GET /api/session/title requests.
// It reads the "title" query parameter as the original title,
// takes the first 5 messages from the session history,
// sends them to the LLM to generate a new concise title,
// and returns the new title as JSON. On error or empty LLM response, returns the original title.
//
// NOTE: This handler does NOT save the generated title to the session.
// The title is only saved when the user explicitly accepts it via PUT /api/session/title.
// This ensures the backend does not modify session state before user confirmation.
func (h *ChatAgent) OnGetSessionTitle(w http.ResponseWriter, r *http.Request) {
	// Only accept GET
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the original title from query parameter
	originalTitle := r.URL.Query().Get("title")

	// Determine the language for this request.
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.mu.Lock()
	samples := extractMessagesForTitle(session.currentChat.history)
	session.mu.Unlock()

	// Build the LLM prompt with i18n support
	systemPrompt := i18n.SystemPrompt.TL(lang, "title")
	var contentBuilder strings.Builder

	for _, msg := range samples {
		switch msg.Role {
		case llm.RoleUser:
			contentBuilder.WriteString("A: ")
		case llm.RoleAssistant:
			contentBuilder.WriteString("B: ")
		}
		contentBuilder.WriteString(msg.Content)
		contentBuilder.WriteString("\n")
	}

	messages := make([]llm.Message, 0, 2)
	messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: contentBuilder.String()})

	newTitle := ""
	titleChanged := false

	// Call LLM (non-streaming) with a 50-second timeout.
	// Title generation is a lightweight task; if it takes longer than 50s,
	// the LLM is likely stuck in a thinking loop, so we time out and
	// fall back to the original title.
	titleCtx, titleCancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer titleCancel()
	resp, err := h.charLLMClient.Chat(titleCtx, messages)

	if err != nil {
		log.Printf("Make char-llm client fail. %v", err)
	} else if len(resp.Choices) > 0 {
		// Extract the reply content
		newTitle = resp.Choices[0].Message.Content
	}

	// Validate the generated title:
	// - If LLM returned empty content, fall back to original title
	// - If the generated title is unreasonably long (>50 runes), the LLM likely
	//   failed to generate a concise title; discard it and use the original title instead.
	//   50 runes ≈ 15 Chinese characters or 8 English words, matching the prompt constraints.
	const maxTitleLen = 50
	if newTitle == "" || len([]rune(newTitle)) > maxTitleLen {
		newTitle = originalTitle
	}

	// Determine if the title changed (for the response only, no session mutation)
	if newTitle != originalTitle {
		titleChanged = true
	}

	// Return the new title as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"title":   newTitle,
		"changed": titleChanged,
	})
}

// ============================================================
// Session pin handler — PUT /api/session/pin?sn=XXX&pinned=true|false
// ============================================================

// OnUpdateSessionPin handles PUT /api/session/pin — toggles the pinned state of a session.
func (h *ChatAgent) OnUpdateSessionPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}

	pinnedStr := r.URL.Query().Get("pinned")
	pinned := pinnedStr == "true"

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.chatStore == nil {
		http.Error(w, "user not logged in", http.StatusBadRequest)
		return
	}

	// Find the session by SN
	var targetSession *store.Session
	for i := range session.chats {
		if session.chats[i].SN == sn {
			targetSession = &session.chats[i]
			break
		}
	}
	if targetSession == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if err := session.chatStore.UpdateSessionPin(targetSession.ID, pinned); err != nil {
		log.Printf("failed to update session pin: %v", err)
		http.Error(w, "failed to update session pin", http.StatusInternalServerError)
		return
	}

	// Update in-memory cache
	targetSession.Pinned = pinned

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// Session delete handler — DELETE /api/session?sn=XXX
// ============================================================

// OnDeleteSession handles DELETE /api/session — logically deletes a session.
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

	session.mu.Lock()
	defer session.mu.Unlock()

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
	filtered := make([]store.Session, 0, len(session.chats))
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
