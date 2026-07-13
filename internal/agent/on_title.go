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

// OnPutChatTitle handles PUT /api/chat/title
func (h *ChatAgent) OnPutChatTitle(w http.ResponseWriter, r *http.Request) {
	newTitle := r.URL.Query().Get("title")
	if newTitle == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "title"}), http.StatusBadRequest)
		return
	}

	stateStr := r.URL.Query().Get("state")
	titleState := TitleStateUserModified
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

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	sess.User.ChatsMu.Lock()

	var targetIndex = -1
	for i := range sess.User.Chats {
		if sess.User.Chats[i].SN == sn {
			targetIndex = i
			break
		}
	}
	if targetIndex == -1 {
		sess.User.ChatsMu.Unlock()
		http.Error(w, i18n.T("db_session_not_found"), http.StatusNotFound)
		return
	}

	targetID := sess.User.Chats[targetIndex].ID
	sess.User.ChatsMu.Unlock()

	if err := theChatStore.UpdateChatTitle(
		targetID,
		newTitle,
		int8(titleState),
	); err != nil {
		http.Error(w, i18n.T("db_update_chat_title_failed"), http.StatusInternalServerError)
		return
	}

	sess.User.ChatsMu.Lock()
	for i := range sess.User.Chats {
		if sess.User.Chats[i].SN == sn {
			sess.User.Chats[i].Title = newTitle
			sess.User.Chats[i].TitleState = int8(titleState)
			break
		}
	}
	sess.User.ChatsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
	})
}

// ============================================================
// Chat title generation handler -GET /api/session/title?title=XXX
// ============================================================

func extractMessagesForTitle(msgs []Message) []Message {
	c := len(msgs)
	if c <= 5 {
		return msgs
	}

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
func (h *ChatAgent) OnGetSuggestedChatTitle(w http.ResponseWriter, r *http.Request) {
	originalTitle := r.URL.Query().Get("title")

	chatSN := r.URL.Query().Get("sn")
	var chatID int64

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	if chatSN != "" {
		sess.User.ChatsMu.Lock()
		var found bool
		for _, c := range sess.User.Chats {
			if c.SN == chatSN {
				found = true
				chatID = c.ID
				break
			}
		}
		sess.User.ChatsMu.Unlock()

		if !found {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"sn":      chatSN,
				"title":   originalTitle,
				"changed": false,
			})
			return
		}
	} else {
		sess.Mu.Lock()
		if sess.User.CurrentChat.DBCHat != nil {
			chatSN = sess.User.CurrentChat.DBCHat.SN
			chatID = sess.User.CurrentChat.DBCHat.ID
		}
		sess.Mu.Unlock()
	}

	var msgs []Message
	if chatID != 0 {
		dbMessages, err := theChatStore.ListMessages(chatID)
		if err != nil {
			http.Error(w, i18n.T("api_error_failed_to_list_messages", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
			return
		}
		agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, theChatStore, chatID)
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

	systemPromptBuilder := &strings.Builder{}
	systemPromptBuilder.WriteString(i18n.SystemPrompt.TL(lang, "title", map[string]any{"Title": originalTitle}))
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

	client := sessionLLMClient(sess)
	llmApiSettings := sessionLLMApiSetting(sess)

	titleCtx, titleCancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer titleCancel()
	resp, err := client.Chat(titleCtx, messages, llmApiSettings.ApiKey)

	if err == nil && len(resp.Choices) > 0 {
		newTitle = resp.Choices[0].Message.Content
	}

	const maxTitleLen = 50.0
	if newTitle == "" || toolset.VisualLength(newTitle) > maxTitleLen {
		newTitle = originalTitle
	}

	if newTitle != originalTitle {
		titleChanged = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sn":      chatSN,
		"title":   newTitle,
		"changed": titleChanged,
	})
}
