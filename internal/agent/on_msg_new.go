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
//
// frontSN is the frontend's chat SN. Used to restore an existing chat from DB
// when the session has lost state (e.g. after backend restart).
func appendNewRequestMessage(sess *session.Session, reqMsg *Message, chatStore *store.ChatStore, frontSN string) (bool, error) {
	if reqMsg.ID != 0 {
		panic(fmt.Sprintf("new request message's ID is not 0, but %d", reqMsg.ID))
	}

	// Use in-memory cache to get the last message ID, avoiding a DB query.
	var lastID int64 = 0
	if len(sess.User.CurrentChat.Messages) > 0 {
		lastMsg := sess.User.CurrentChat.Messages[len(sess.User.CurrentChat.Messages)-1]
		lastID = lastMsg.ID
	}

	reqMsg.ID = lastID + 1

	isNewChat, err := ensureSessionDBForChat(sess, chatStore, frontSN)
	if err != nil {
		return false, err
	}
	if sess.User.CurrentChat.DBCHat == nil {
		return false, fmt.Errorf("dBCHat is nil after ensureSessionDBForChat")
	}

	chatID := sess.User.CurrentChat.DBCHat.ID
	persistMessageToDB(sess, reqMsg, chatID, chatStore)

	// Append the new user message to the in-memory cache.
	sess.User.CurrentChat.Messages = append(sess.User.CurrentChat.Messages, *reqMsg)

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
//
// cachedMsgs is a copy of the in-memory message cache, safe to use after
// the lock is released. The caller must use this instead of querying the DB.
func prepareMessageForLLM(sess *session.Session, reqMsg *Message, frontSN string, chatStore *store.ChatStore, sseWriter *sse.Writer, lang string) (msgChatID int64, cachedMsgs []Message, chatCreatedSN string, chatCreatedFrontSN string, ok bool) {
	sess.Mu.Lock()
	defer sess.Mu.Unlock()

	isNewChat, err := appendNewRequestMessage(sess, reqMsg, chatStore, frontSN)
	if err != nil {
		sseWriter.WriteEvent(ErrorEvent{
			Type:    "error",
			Message: i18n.TL(lang, "api_error_failed_to_create_session"),
		})
		return 0, nil, "", "", false
	}

	if reqMsg.ID <= 0 {
		panic("new message's ID is zero still after append to messages")
	}

	// Copy cached messages for use outside the lock (prevents race conditions).
	cachedMsgs = make([]Message, len(sess.User.CurrentChat.Messages))
	copy(cachedMsgs, sess.User.CurrentChat.Messages)

	if isNewChat && sess.User.CurrentChat.DBCHat != nil && sess.User.CurrentChat.DBCHat.SN != "" {
		chatCreatedSN = sess.User.CurrentChat.DBCHat.SN
		chatCreatedFrontSN = frontSN
	}

	if sess.User.CurrentChat.DBCHat != nil {
		msgChatID = sess.User.CurrentChat.DBCHat.ID
	}

	return msgChatID, cachedMsgs, chatCreatedSN, chatCreatedFrontSN, true
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

	msgChatID, cachedMsgs, chatCreatedSN, chatCreatedFrontSN, ok := prepareMessageForLLM(sess, &req.Message, req.FrontSN, theChatStore, sseWriter, lang)
	if !ok {
		return
	}

	// Use in-memory cached messages instead of querying the DB.
	llmMsgs := llmtypes.ConvertAgentMessagesToLLMMessages(cachedMsgs)

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
			ID:      msgChatID,
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
		// Update cache only if the current chat hasn't been switched during streaming.
		if sess.User.CurrentChat.DBCHat != nil && sess.User.CurrentChat.DBCHat.ID == msgChatID {
			sess.User.CurrentChat.Messages = append(sess.User.CurrentChat.Messages, *assistantMsg)
		}
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
