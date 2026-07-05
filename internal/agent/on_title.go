package agent

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/toolset"
)

// ============================================================
// PutChatTitle handler -PUT /api/chat/title?title=XXX&state=N&sn=XXX
// ============================================================

// OnPutChatTitle handles PUT /api/chat/title -updates the chat title
// and marks the title state.
// Query parameters:
//
//	title -the new title to set (required)
//	state -title modification state: 0=original, 1=AI-modified, 2=user-modified (default: 2)
//	sn    -the target chat SN (required)
//
// Returns HTTP 200 on success.
func (h *ChatAgent) OnPutChatTitle(w http.ResponseWriter, r *http.Request) {
	// Read the new title from query parameter
	newTitle := r.URL.Query().Get("title")
	if newTitle == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "title"}), http.StatusBadRequest)
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

	// Read the required sn parameter -update a specific session from the list
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Update a specific session from the user's session list (e.g., rename from sidebar).
	// Uses chatsMu (independent of session.mu) to avoid blocking streaming.
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
		http.Error(w, i18n.T("db_session_not_found"), http.StatusNotFound)
		return
	}

	// Capture needed data under lock, then release immediately
	targetID := session.chats[targetIndex].ID
	session.chatsMu.Unlock()

	chatStore, cerr := h.openChatDB(session)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}

	// DB write outside lock (different session_id, no conflict with streaming)
	if err := chatStore.UpdateChatTitle(
		targetID,
		newTitle,
		int8(titleState),
	); err != nil {
		h.closeChatDB(chatStore)
		http.Error(w, i18n.T("db_update_chat_title_failed"), http.StatusInternalServerError)
		return
	}
	h.closeChatDB(chatStore)

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

	// Return simple 200 OK
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// Chat title generation handler -GET /api/session/title?title=XXX
// ============================================================

// extractMessagesForTitle extracts a representative sample of messages
// for LLM-based title generation. When messages are short (<=50 messages),
// all messages are used. For longer message lists, a sampling strategy is
// applied to include the first, last, and representative intermediate messages.
//
// For AI messages in the middle portion, a randomized sampling (~/3 probability)
// is used instead of a fixed ID%3==0 pattern. This ensures that when a user
// extractMessagesForTitle returns a representative subset of messages for title generation.
// It always includes the first 5 messages and the last message, and randomly samples
// intermediate assistant messages to provide diverse context. Content exceeding 1024 runes
// is truncated. This diversity helps produce varied title candidates across multiple calls.
func extractMessagesForTitle(msgs []Message) []Message {
	c := len(msgs)
	if c <= 5 {
		return msgs
	}

	// Use a local random source seeded with current time (nanosecond precision)
	// so each invocation gets a different sampling pattern.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	i := 5
	samples := msgs[0:i]

	count, last := i, c-1

	for j := i + 1; j < last; j++ {
		msg := msgs[j]

		ml := 1024
		if count > 25 {
			ml = 512
		}
		if utf8.RuneCountInString(msg.Content) > ml {
			msg.Content = string([]rune(msg.Content)[:ml]) + "..."
		}

		if msg.Role == llm.RoleAssistant {
			if count > 50 {
				if rng.Intn(3) != 1 {
					continue
				}
			}

			msg.Reasoning = ""
		}

		samples = append(samples, msg)
		count++
	}

	samples = append(samples, msgs[last])
	return samples
}

// OnGetSuggestedChatTitle handles GET /api/session/title requests.
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
func (h *ChatAgent) OnGetSuggestedChatTitle(w http.ResponseWriter, r *http.Request) {
	// Read the original title from query parameter
	originalTitle := r.URL.Query().Get("title")

	// Read the optional sn parameter -if provided, generate a title
	// for that specific chat instead of the current active chat.
	chatSN := r.URL.Query().Get("sn")
	var chatID int64

	// Determine the language for this request.
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Resolve which chat's messages to use
	if chatSN != "" {
		// Verify the chat exists by SN and get its ID
		session.chatsMu.Lock()
		var found bool
		for _, c := range session.chats {
			if c.SN == chatSN {
				found = true
				chatID = c.ID
				break
			}
		}
		session.chatsMu.Unlock()

		if !found {
			// Chat not found (may have been deleted) -return original title
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
			chatSN = session.currentChat.dbChat.SN
			chatID = session.currentChat.dbChat.ID
		}
		session.mu.Unlock()
	}

	var msgs []Message
	if chatID != 0 {
		chatStore, cerr := h.openChatDB(session)
		if cerr != nil {
			http.Error(w, i18n.T("api_error_failed_to_open_chat_store_detail", map[string]any{"Error": cerr.Error()}), http.StatusInternalServerError)
			return
		}

		dbMessages, err := chatStore.ListMessages(chatID)
		if err != nil {
			h.closeChatDB(chatStore)
			http.Error(w, i18n.T("api_error_failed_to_list_messages", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
			return
		}
		agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, chatStore, chatID)
		h.closeChatDB(chatStore)
		if convErr != nil {
			http.Error(w, i18n.T("api_error_failed_to_load_web_sources", map[string]any{"Error": convErr.Error()}), http.StatusInternalServerError)
			return
		}
		msgs = agentMsgs
	}
	if msgs == nil {
		msgs = []Message{}
	}
	samples := extractMessagesForTitle(msgs)
	msgs = nil

	// Build the LLM prompt with i18n support
	systemPromptBuilder := &strings.Builder{}
	systemPromptBuilder.WriteString(i18n.SystemPrompt.TL(lang, "title", map[string]interface{}{"Title": originalTitle}))
	systemPromptBuilder.WriteString("\n------")

	for _, msg := range samples {
		switch msg.Role {
		case llm.RoleUser:
			systemPromptBuilder.WriteString("\nA: ")
		case llm.RoleAssistant:
			systemPromptBuilder.WriteString("\nB: ")
		}

		systemPromptBuilder.WriteString(msg.Content)
	}

	messages := make([]llm.Message, 1)
	messages[0] = llm.Message{Role: llm.RoleSystem, Content: systemPromptBuilder.String()}

	newTitle := ""
	titleChanged := false

	// Call LLM (non-streaming) with a 50-second timeout.
	// Title generation is a lightweight task; if it takes longer than 50s,
	// the LLM is likely stuck in a thinking loop, so we time out and
	// fall back to the original title.
	titleCtx, titleCancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer titleCancel()
	resp, err := h.charLLMClient.Chat(titleCtx, messages)

	if err == nil && len(resp.Choices) > 0 {
		// Extract the reply content
		newTitle = resp.Choices[0].Message.Content
	}

	// Validate the generated title:
	// - If LLM returned empty content, fall back to original title
	// - If the generated title is unreasonably long (>50 visual length), the LLM likely
	//   failed to generate a concise title; discard it and use the original title instead.
	//   50 visual length ~33 Chinese characters or 50 English chars, matching the prompt constraints.
	const maxTitleLen = 50.0
	if newTitle == "" || toolset.VisualLength(newTitle) > maxTitleLen {
		newTitle = originalTitle
	}

	// Determine if the title changed (for the response only, no session mutation)
	if newTitle != originalTitle {
		titleChanged = true
	}

	// Return the new title and the SN as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sn":      chatSN,
		"title":   newTitle,
		"changed": titleChanged,
	})
}
