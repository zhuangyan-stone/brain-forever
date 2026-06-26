package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/local/agent/toolimp"
)

// ============================================================
// ChatHandler -POST /api/chat handler (core)
// ============================================================

// appendNewRequestMessage enqueues a new user message, assigns an ID,
// ensures a DB session exists, and persists the message to DB.
// Must be called with session.mu held.
// Returns true if a new DB chat session was created as a result of this call.
func appendNewRequestMessage(session *session, reqMsg *Message) bool {
	if reqMsg.ID != 0 {
		panic(fmt.Sprintf("new request message's ID is not 0, but %d", reqMsg.ID))
	}

	var lastID int64 = 0

	// Load the last message from DB to determine the next ID
	var dbSessionID int64
	if session.currentChat.dbChat != nil {
		dbSessionID = session.currentChat.dbChat.ID
	}
	if dbSessionID > 0 {
		dbMessages, err := session.chatsStore.ListMessages(dbSessionID)
		if err == nil && len(dbMessages) > 0 {
			lastMsg := dbMessages[len(dbMessages)-1]
			lastID = int64(lastMsg.GroupIndex)
		}
	}

	reqMsg.ID = lastID + 1

	// Ensure a DB session record exists
	isNewChat := ensureSessionDBForChat(session)

	// Persist the user message to DB
	// session.mu is held, so session.currentChat is stable
	chatID := session.currentChat.dbChat.ID
	persistMessageToDB(session, reqMsg, chatID)

	return isNewChat
}

// resolveNewMessageRequest parses and validates the incoming chat request.
// Returns nil if validation fails (the caller should return immediately in that case).
func (h *ChatAgent) resolveNewMessageRequest(w http.ResponseWriter, r *http.Request) *ChatRequest {
	// Only accept POST
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil
	}

	// Parse request
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse request. %v", err), http.StatusBadRequest)
		return nil
	}

	if req.Message.Content == "" {
		http.Error(w, "message content cannot be empty", http.StatusBadRequest)
		return nil
	} else if req.Message.ID != 0 {
		http.Error(w, "new message's id  must be zero", http.StatusBadRequest)
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

	session.mu.Lock()

	// 3. Determine the language for this request.
	// Priority: request header Accept-Language > handler defaultLang > "en"
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// 4. Add the message to the messages and assign it a unique ID
	//    isNewChat indicates whether a new DB chat session was created by this call.
	isNewChat := appendNewRequestMessage(session, &req.Message)

	if req.Message.ID <= 0 {
		session.mu.Unlock()
		panic("new message's ID is zero still after append to messages")
	}

	// 5. 在释放 mu 前捕获新 Chat 的相关信息（SN 和 FrontSN），
	//    用于流式完成后决定是否发送 chat_created 事件。
	//    session.mu 在流式期间不持有，其他 handler 可能在此窗口期改变 currentChat，
	//    因此必须在此处捕获，不能等到流式结束后再读取。
	var chatCreatedSN string
	var chatCreatedFrontSN string
	if isNewChat && session.currentChat.dbChat != nil && session.currentChat.dbChat.SN != "" {
		chatCreatedSN = session.currentChat.dbChat.SN
		chatCreatedFrontSN = req.FrontSN
	}

	// 6. 在释放 mu 前捕获当前 Chat 的 DB ID，用于流式完成后 persist assistant 消息
	//    session.mu 在流式期间不持有，OnSwitchChat 可能在此期间改变 currentChat）
	//    导致 persistMessageToDB 将 assistant 写入错误的 Chat。
	var msgChatID int64
	if session.currentChat.dbChat != nil {
		msgChatID = session.currentChat.dbChat.ID
	}

	// 7. Load messages from DB for the LLM call
	llmMsgs, err := loadMessagesAsLLMMessages(session)
	session.mu.Unlock()

	if err != nil {
		http.Error(w, fmt.Sprintf("failed to load messages: %v", err), http.StatusInternalServerError)
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
		webSearchToolImp := toolimp.MakeWebSearchTool(r.Context(), h.webSearcher, lang)
		toolsImp = append(toolsImp, webSearchToolImp)
	}

	if req.TraitSearchEnabled {
		traitSearcher := &traitSearchAdapter{
			client: h.embedder,
			store:  session.traitsStore,
		}
		traitSearchByTextToolImp := toolimp.MakeTraitSearchByTextTool(r.Context(), traitSearcher, lang)
		traitSearchByKeywordToolImp := toolimp.MakeTraitSearchByKeywordTool(r.Context(), traitSearcher, lang)
		toolsImp = append(toolsImp, traitSearchByTextToolImp, traitSearchByKeywordToolImp)
	}

	// 10. Only send chat_created event if a new DB chat was actually created by this request.
	//     ★ Fix: Use the isNewChat flag to decide, instead of re-reading currentChat without holding the lock.
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
		lang)

	// 12. Persist the assistant message to DB
	//  Use the chatID captured when streaming started, not session.currentChat,
	//  to avoid persisting to the wrong conversation if the user switches chats
	//  before the flow completes.
	if assistantMsg != nil {
		session.mu.Lock()
		persistMessageToDB(session, assistantMsg, msgChatID)
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
