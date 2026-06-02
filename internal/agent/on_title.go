package agent

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
)

// ============================================================
// PutChatTitle handler — PUT /api/session/title?title=XXX&state=N
// ============================================================

// OnPutChatTitle handles PUT /api/session/title — updates the chat title
// and marks the title state.
// Query parameters:
//
//	title — the new title to set (required)
//	state — title modification state: 0=original, 1=AI-modified, 2=user-modified (default: 2)
//
// Returns HTTP 200 on success.
func (h *ChatAgent) OnPutChatTitle(w http.ResponseWriter, r *http.Request) {
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

	// ------------------------------------------
	// sn != "" : Sidebar rename (historical session)
	//   → Uses chatsMu: only protects chats slice + chatStore (independent of streaming)
	// sn == "" : Header title edit (current active session)
	//   → Uses mu: protects currentChat.title + titleState
	// ------------------------------------------
	if sn != "" {
		// Update a specific session from the user's session list (e.g., rename from sidebar)
		// This operates on a different session_id from any in-progress streaming,
		// so it uses chatsMu (independent of session.mu) to avoid blocking streaming.

		session.chatsMu.Lock()

		// Find the session by SN (under lock)
		var targetIndex = -1
		for i := range session.chats {
			if session.chats[i].SN == sn {
				targetIndex = i
				break
			}
		}
		if targetIndex == -1 {
			session.chatsMu.Unlock()
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		// Capture needed data under lock, then release immediately
		targetID := session.chats[targetIndex].ID
		chatStore := session.chatStore
		session.chatsMu.Unlock()

		// DB write outside lock (different session_id, no conflict with streaming)
		if err := chatStore.UpdateChatTitle(
			targetID,
			newTitle,
			int8(titleState),
		); err != nil {
			log.Printf("failed to update session title in DB: %v", err)
			http.Error(w, "failed to update session title", http.StatusInternalServerError)
			return
		}

		// Re-acquire lock briefly to update in-memory cache
		session.chatsMu.Lock()
		for i := range session.chats {
			if session.chats[i].SN == sn {
				session.chats[i].Title = newTitle
				session.chats[i].TitleState = int8(titleState)
				break
			}
		}
		session.chatsMu.Unlock()
	} else {
		// Update the current active session title atomically (title + state).
		// This uses session.mu because it modifies currentChat, which is also
		// accessed by OnNewMessage during streaming.
		session.mu.Lock()
		session.currentChat.title = newTitle
		if titleState > session.currentChat.titleState {
			session.currentChat.titleState = titleState
		}

		var dbSessionID int64
		if session.currentChat.dbChat != nil {
			dbSessionID = session.currentChat.dbChat.ID
		}

		// Sync title to DB if the current chat has a DB session
		if dbSessionID != 0 {
			if err := session.chatStore.UpdateChatTitle(
				dbSessionID,
				newTitle,
				int8(titleState),
			); err != nil {
				log.Printf("failed to update session title in DB: %v", err)
			}
		}
		session.mu.Unlock()

		// Sync the title to the in-memory chat list (sess.chats) so that
		// subsequent GET /api/session calls return the correct title for the sidebar.
		session.syncCurrentChatTitleToChatList(newTitle, int(titleState))
	}

	// Return simple 200 OK
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// Chat title generation handler — GET /api/session/title?title=XXX
// ============================================================

// extractMessagesForTitle extracts a representative sample of messages
// for LLM-based title generation. When messages are short (<=50 messages),
// all messages are used. For longer message lists, a sampling strategy is
// applied to include the first, last, and representative intermediate messages.
//
// For AI messages in the middle portion, a randomized sampling (≈1/3 probability)
// is used instead of a fixed ID%3==0 pattern. This ensures that when a user
// requests title suggestions multiple times, different message subsets are fed
// to the LLM, producing more diverse title candidates.
func extractMessagesForTitle(msgs []Message) []Message {
	c := len(msgs)
	if c <= 50 {
		return msgs
	}

	// Use a local random source seeded with current time (nanosecond precision)
	// so each invocation gets a different sampling pattern.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	i := c/3 + 1
	samples := msgs[0:i]

	for j := i + 1; j < c-1; j++ {
		if msgs[j].Role == llm.RoleUser {
			samples = append(samples, msgs[j])
		} else if rng.Intn(3) == 0 {
			msg := msgs[j]
			msg.Reasoning = ""

			if utf8.RuneCountInString(msg.Content) > 600 {
				msg.Content = string([]rune(msg.Content)[:600])
			}

			samples = append(samples, msg)
		}
	}

	samples = append(samples, msgs[c-1])
	return samples
}

// OnProposeChatTitle handles GET /api/session/title requests.
// It reads the "title" query parameter as the original title,
// and optionally a "sn" parameter specifying which chat to generate a title for.
// If "sn" is provided, messages are loaded from that specific chat.
// If "sn" is omitted (or empty), the current active chat's messages are used (backward compatible).
//
// It sends the messages to the LLM to generate a new concise title,
// and returns the new title along with the chat SN (so the frontend can
// correctly identify which chat to update) as JSON.
// On error or empty LLM response, returns the original title.
//
// NOTE: This handler does NOT save the generated title to the session.
// The title is only saved when the user explicitly accepts it via PUT /api/session/title.
// This ensures the backend does not modify session state before user confirmation.
func (h *ChatAgent) OnProposeChatTitle(w http.ResponseWriter, r *http.Request) {
	// Only accept GET
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the original title from query parameter
	originalTitle := r.URL.Query().Get("title")

	// Read the optional sn parameter — if provided, generate a title
	// for that specific chat instead of the current active chat.
	chatSN := r.URL.Query().Get("sn")

	// Determine the language for this request.
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Resolve which chat's messages to use
	var dbSessionID int64
	if chatSN != "" {
		// Look up the chat by SN from the session's chat list
		session.chatsMu.Lock()
		for _, c := range session.chats {
			if c.SN == chatSN {
				dbSessionID = c.ID
				break
			}
		}
		session.chatsMu.Unlock()

		if dbSessionID == 0 {
			// Chat not found (may have been deleted) — return original title
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"sn":      chatSN,
				"title":   originalTitle,
				"changed": false,
			})
			return
		}
	} else {
		// No sn provided: use the current active chat (backward compatible)
		session.mu.Lock()
		if session.currentChat.dbChat != nil {
			dbSessionID = session.currentChat.dbChat.ID
		}
		session.mu.Unlock()
	}

	var msgs []Message
	if dbSessionID > 0 {
		dbMessages, err := session.chatStore.ListMessages(dbSessionID)
		if err == nil {
			msgs = convertDBMessagesToAgentMessages(dbMessages, session.chatStore, dbSessionID)
		}
	}
	if msgs == nil {
		msgs = []Message{}
	}
	samples := extractMessagesForTitle(msgs)

	// Build the LLM prompt with i18n support
	systemPrompt := i18n.SystemPrompt.TL(lang, "title")

	messages := make([]llm.Message, 0, 1+len(samples))
	messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	for _, msg := range samples {
		messages = append(messages, llm.Message{Role: msg.Role, Content: msg.Content})
	}

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

	// Resolve the SN to return — use the requested chatSN if provided,
	// otherwise read the current chat's SN from the session.
	responseSN := chatSN
	if responseSN == "" {
		session.mu.Lock()
		if session.currentChat.dbChat != nil {
			responseSN = session.currentChat.dbChat.SN
		}
		session.mu.Unlock()
	}

	// Return the new title and the SN as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sn":      responseSN,
		"title":   newTitle,
		"changed": titleChanged,
	})
}
