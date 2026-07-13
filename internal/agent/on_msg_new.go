package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/llmtypes"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
)

// ============================================================
// ChatHandler -POST /api/chat handler (core)
// ============================================================

// appendNewRequestMessage enqueues a new user message, assigns an ID,
// ensures a DB session exists, and persists the message to DB.
// Must be called with Mu held.
// Returns true if a new DB chat session was created as a result of this call.
// Returns an error if the DB session could not be created.
func appendNewRequestMessage(sess *session.Session, reqMsg *Message, chatStore *store.ChatStore) (bool, error) {
	if reqMsg.ID != 0 {
		panic(fmt.Sprintf("new request message's ID is not 0, but %d", reqMsg.ID))
	}

	var lastID int64 = 0

	var dbChatID int64
	if sess.User.CurrentChat.DBCHat != nil {
		dbChatID = sess.User.CurrentChat.DBCHat.ID
	}
	if dbChatID != 0 {
		dbMessages, err := chatStore.ListMessages(dbChatID)
		if err == nil && len(dbMessages) > 0 {
			lastMsg := dbMessages[len(dbMessages)-1]
			lastID = int64(lastMsg.GroupIndex)
		}
	}

	reqMsg.ID = lastID + 1

	isNewChat, err := ensureSessionDBForChat(sess, chatStore)
	if err != nil {
		return false, err
	}
	if sess.User.CurrentChat.DBCHat == nil {
		return false, fmt.Errorf("DBCHat is nil after ensureSessionDBForChat")
	}

	chatID := sess.User.CurrentChat.DBCHat.ID
	persistMessageToDB(sess, reqMsg, chatID, chatStore)

	return isNewChat, nil
}

// resolveNewMessageRequest parses and validates the incoming chat request.
func (h *ChatAgent) resolveNewMessageRequest(w http.ResponseWriter, r *http.Request) *ChatRequest {
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

// prepareMessageForLLM performs the locked section of OnNewMessage.
// It acquires and releases the session lock internally.
// Calls appendNewRequestMessage (which requires Mu held), then extracts
// data needed after the lock is released.
// Returns ok=false if an error occurred (the error event has already been
// sent via sseWriter).
func prepareMessageForLLM(sess *session.Session, reqMsg *Message, frontSN string, chatStore *store.ChatStore, sseWriter *sse.Writer, lang string) (msgChatID int64, chatCreatedSN string, chatCreatedFrontSN string, ok bool) {
	sess.Mu.Lock()
	defer sess.Mu.Unlock()

	isNewChat, err := appendNewRequestMessage(sess, reqMsg, chatStore)
	if err != nil {
		sseWriter.WriteEvent(ErrorEvent{
			Type:    "error",
			Message: i18n.TL(lang, "api_error_failed_to_create_session"),
		})
		return 0, "", "", false
	}

	if reqMsg.ID <= 0 {
		panic("new message's ID is zero still after append to messages")
	}

	if isNewChat && sess.User.CurrentChat.DBCHat != nil && sess.User.CurrentChat.DBCHat.SN != "" {
		chatCreatedSN = sess.User.CurrentChat.DBCHat.SN
		chatCreatedFrontSN = frontSN
	}

	if sess.User.CurrentChat.DBCHat != nil {
		msgChatID = sess.User.CurrentChat.DBCHat.ID
	}

	return msgChatID, chatCreatedSN, chatCreatedFrontSN, true
}

// OnNewMessage handles POST /api/chat requests
func (h *ChatAgent) OnNewMessage(w http.ResponseWriter, r *http.Request) {
	req := h.resolveNewMessageRequest(w, r)
	if req == nil {
		return
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// Create SSE writer early so we can send an error event to the frontend
	// if session creation fails, preventing the frontend from hanging.
	sseWriter := sse.NewSSEWriter(w)

	msgChatID, chatCreatedSN, chatCreatedFrontSN, ok := prepareMessageForLLM(sess, &req.Message, req.FrontSN, theChatStore, sseWriter, lang)
	if !ok {
		return
	}
	llmChatID := msgChatID

	llmMsgs, err := llmtypes.LoadMessagesAsLLMMessages(llmChatID, theChatStore)
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

	timeQueryToolImp := toolimp.MakeTimeQueryTool(lang)
	toolsImp := []llm.ToolIMP{timeQueryToolImp}

	if req.WebSearchEnabled {
		webSearcher := sessionWebSearcher(sess)
		webSearchToolImp := toolimp.MakeWebSearchTool(r.Context(), webSearcher, lang)
		toolsImp = append(toolsImp, webSearchToolImp)
	}

	if req.TraitSearchEnabled {
		embedder := sessionEmbedder(sess)
		embedderSetting := sessionEmbedderApiSetting(sess)
		traitSearcher := &traitSearchAdapter{
			client:     embedder,
			store:      theBrainStore,
			lang:       lang,
			userID:     sess.User.ID,
			apiSetting: embedderSetting,
		}
		traitSearchByTextToolImp := toolimp.MakeTraitSearchByTextTool(r.Context(), traitSearcher, lang)
		traitSearchByKeywordToolImp := toolimp.MakeTraitSearchByKeywordTool(r.Context(), traitSearcher, lang)
		toolsImp = append(toolsImp, traitSearchByTextToolImp, traitSearchByKeywordToolImp)
	}

	if chatCreatedSN != "" {
		sseWriter.WriteEvent(ChatCreatedEvent{
			Type:    "chat_created",
			SN:      chatCreatedSN,
			FrontSN: chatCreatedFrontSN,
		})
	}

	assistantMsg := h.callLLMWithPipeline(r.Context(), sseWriter,
		req.Message.ID,
		messages,
		toolsImp,
		req.DeepThink,
		lang,
		sess)

	if assistantMsg != nil {
		sess.Mu.Lock()
		persistMessageToDB(sess, assistantMsg, msgChatID, theChatStore)
		sess.Mu.Unlock()
	}
}

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
