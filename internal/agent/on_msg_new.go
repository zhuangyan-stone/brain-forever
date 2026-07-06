package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
)

// ============================================================
// ChatHandler -POST /api/chat handler (core)
// ============================================================

// appendNewRequestMessage enqueues a new user message, assigns an ID,
// ensures a DB session exists, and persists the message to DB.
// Must be called with session.mu held.
// Returns true if a new DB chat session was created as a result of this call.
func appendNewRequestMessage(session *session, reqMsg *Message, chatStore *store.ChatStore) bool {
	if reqMsg.ID != 0 {
		panic(fmt.Sprintf("new request message's ID is not 0, but %d", reqMsg.ID))
	}

	var lastID int64 = 0

	// Load the last message from DB to determine the next ID
	var dbChatID int64
	if session.user.currentChat.dbChat != nil {
		dbChatID = session.user.currentChat.dbChat.ID
	}
	if dbChatID != 0 {
		dbMessages, err := chatStore.ListMessages(dbChatID)
		if err == nil && len(dbMessages) > 0 {
			lastMsg := dbMessages[len(dbMessages)-1]
			lastID = int64(lastMsg.GroupIndex)
		}
	}

	reqMsg.ID = lastID + 1

	// Ensure a DB session record exists
	isNewChat := ensureSessionDBForChat(session, chatStore)
	// Persist the user message to DB
	// session.mu is held, so session.user.currentChat is stable
	chatID := session.user.currentChat.dbChat.ID
	persistMessageToDB(session, reqMsg, chatID, chatStore)

	return isNewChat
}

// resolveNewMessageRequest parses and validates the incoming chat request.
// Returns nil if validation fails (the caller should return immediately in that case).
func (h *ChatAgent) resolveNewMessageRequest(w http.ResponseWriter, r *http.Request) *ChatRequest {
	// Parse request
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return nil
	}

	if req.Message.Content == "" {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "message content cannot be empty"}), http.StatusBadRequest)
		return nil
	} else if req.Message.ID != 0 {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "new message's id must be zero"}), http.StatusBadRequest)
		return nil
	}

	return &req
}

// OnNewMessage handles POST /api/chat requests
func (h *ChatAgent) OnNewMessage(w http.ResponseWriter, r *http.Request) {
	// 1. Resolve request
	req := h.resolveNewMessageRequest(w, r)
	if req == nil {
		return
	}

	// 2. Resolve sessionID from cookie (or generate a new one) and get/create the session
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Open chatStore on-demand for DB operations during this request
	chatStore, err := h.openChatDB(session)
	if err != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store_detail", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	session.mu.Lock()

	// 3. Determine the language for this request.
	// Priority: request header Accept-Language > handler defaultLang > "en"
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// 4. Add the message to the messages and assign it a unique ID
	//    isNewChat indicates whether a new DB chat session was created by this call.
	isNewChat := appendNewRequestMessage(session, &req.Message, chatStore)

	if req.Message.ID <= 0 {
		session.mu.Unlock()
		panic("new message's ID is zero still after append to messages")
	}

	// 5. Capture chat SN and FrontSN before releasing mu,
	//    used to decide whether to send chat_created event after streaming.
	//    session.mu is not held during streaming, other handlers may change currentChat.
	var chatCreatedSN string
	var chatCreatedFrontSN string
	if isNewChat && session.user.currentChat.dbChat != nil && session.user.currentChat.dbChat.SN != "" {
		chatCreatedSN = session.user.currentChat.dbChat.SN
		chatCreatedFrontSN = req.FrontSN
	}

	// 6. Capture current chat ID before releasing mu, used to persist assistant message after streaming.
	//    session.mu is not held during streaming, OnSwitchChat may change currentChat,
	//    causing persistMessageToDB to write assistant to wrong chat.
	var msgChatID int64
	if session.user.currentChat.dbChat != nil {
		msgChatID = session.user.currentChat.dbChat.ID
	}

	// 7. Load messages from DB for the LLM call
	llmMsgs, err := loadMessagesAsLLMMessages(session, chatStore)
	session.mu.Unlock()

	if err != nil {
		http.Error(w, i18n.T("api_error_failed_to_list_messages", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	startSystemMsg := llm.Message{
		Role:    llm.RoleSystem,
		Content: makeSystemPromptContent(lang, req.TraitSearchEnabled, req.WebSearchEnabled),
	}
	messages := make([]llm.Message, 0, 1+len(llmMsgs))
	messages = append(messages, startSystemMsg)
	messages = append(messages, llmMsgs...)

	// 8. Create SSE writer
	sseWriter := sse.NewSSEWriter(w)

	// 9. Build tool definitions with translated descriptions.
	// time_query tool is always available.
	// web_search tool is only provided when WebSearchEnabled is true.
	// trait_search tools are only provided when TraitSearchEnabled is true.
	timeQueryToolImp := toolimp.MakeTimeQueryTool(lang)
	toolsImp := []llm.ToolIMP{timeQueryToolImp}

	if req.WebSearchEnabled {
		webSearcher := sessionWebSearcher(session)
		webSearchToolImp := toolimp.MakeWebSearchTool(r.Context(), webSearcher, lang)
		toolsImp = append(toolsImp, webSearchToolImp)
	}

	if req.TraitSearchEnabled {
		traitsStore, terr := h.openBrainDB(session)
		if terr != nil {
			h.logger.Errorf("failed to open traits store: %v", terr)
		}
		// Note: traitsStore may be nil if the open failed; in that case,
		// trait search tools won't find any traits (harmless).
		if traitsStore != nil {
			defer h.closeBrainDB(traitsStore)
		}

		embedder := sessionEmbedder(session)
		traitSearcher := &traitSearchAdapter{
			client: embedder,
			store:  traitsStore,
			lang:   lang,
		}
		traitSearchByTextToolImp := toolimp.MakeTraitSearchByTextTool(r.Context(), traitSearcher, lang)
		traitSearchByKeywordToolImp := toolimp.MakeTraitSearchByKeywordTool(r.Context(), traitSearcher, lang)
		toolsImp = append(toolsImp, traitSearchByTextToolImp, traitSearchByKeywordToolImp)
	}

	// 10. Only send chat_created event if a new DB chat was actually created by this request.
	//     -- Fix: Use the isNewChat flag to decide, instead of re-reading currentChat without holding the lock.
	//     The old code re-read currentChat under lock after streaming had started, at which point
	//     currentChat might have been modified by other handlers (e.g., OnSwitchChat, OnNewChat), causing:
	//       a) Every message in an existing chat erroneously sends a chat_created event (wasteful but harmless)
	//       b) More critically, if currentChat has been reset to an empty chat (&chat{}), the
	//          chat_created event is never sent, while the frontend still uses a temporary SN,
	//          preventing subsequent SSE events from finding the correct ChatData via getOrCreate(sn).
	//     New approach: Capture isNewChat and the chat SN under session.mu protection, and determine
	//     whether a chat_created event needs to be sent before streaming begins, eliminating the race window.
	if chatCreatedSN != "" {
		sseWriter.WriteEvent(ChatCreatedEvent{
			Type:    "chat_created",
			SN:      chatCreatedSN,
			FrontSN: chatCreatedFrontSN,
		})
	}

	// 11. Stream with tool call handling
	// callLLMWithPipeline handles the streaming LLM call, tool loop, and SSE events.
	// It returns the assistant message that the caller persists to DB.
	// NOTE: session.mu is NOT held during streaming, allowing other handlers
	// (e.g., OnSwitchChat) to proceed concurrently.
	assistantMsg := h.callLLMWithPipeline(r.Context(), sseWriter,
		req.Message.ID,
		messages,
		toolsImp,
		req.DeepThink,
		lang,
		session)

	// 12. Persist the assistant message to DB
	//  Use the chatID captured when streaming started, not session.user.currentChat,
	//  to avoid persisting to the wrong conversation if the user switches chats
	//  before the flow completes.
	if assistantMsg != nil {
		session.mu.Lock()
		persistMessageToDB(session, assistantMsg, msgChatID, chatStore)
		session.mu.Unlock()
	}
}

// makeSystemPromptContent returns the system prompt content string,
// translated according to the given language.
// Sections are composable: base prompt + optional trait section + optional web search section.
func makeSystemPromptContent(lang string, traitSearchEnabled bool, webSearchEnabled bool) string {
	var sb strings.Builder
	sb.WriteString(i18n.SystemPrompt.TL(lang, "chat"))
	if traitSearchEnabled {
		sb.WriteString(i18n.SystemPrompt.TL(lang, "chat_trait_section"))
	}
	if webSearchEnabled {
		sb.WriteString(i18n.SystemPrompt.TL(lang, "chat_web_section"))
	}
	return sb.String()
}
