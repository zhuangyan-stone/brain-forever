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
func appendNewRequestMessage(sess *session.Session, reqMsg *Message, chatStore *store.ChatStore) bool {
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

	isNewChat := ensureSessionDBForChat(sess, chatStore)
	chatID := sess.User.CurrentChat.DBCHat.ID
	persistMessageToDB(sess, reqMsg, chatID, chatStore)

	return isNewChat
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

// OnNewMessage handles POST /api/chat requests
func (h *ChatAgent) OnNewMessage(w http.ResponseWriter, r *http.Request) {
	req := h.resolveNewMessageRequest(w, r)
	if req == nil {
		return
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	chatStore, err := h.openChatDB(sess)
	if err != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store_detail", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	sess.Mu.Lock()

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	isNewChat := appendNewRequestMessage(sess, &req.Message, chatStore)

	if req.Message.ID <= 0 {
		sess.Mu.Unlock()
		panic("new message's ID is zero still after append to messages")
	}

	var chatCreatedSN string
	var chatCreatedFrontSN string
	if isNewChat && sess.User.CurrentChat.DBCHat != nil && sess.User.CurrentChat.DBCHat.SN != "" {
		chatCreatedSN = sess.User.CurrentChat.DBCHat.SN
		chatCreatedFrontSN = req.FrontSN
	}

	var msgChatID int64
	if sess.User.CurrentChat.DBCHat != nil {
		msgChatID = sess.User.CurrentChat.DBCHat.ID
	}

	// Extract chatID for loading messages, then release lock
	llmChatID := msgChatID
	sess.Mu.Unlock()

	llmMsgs, err := llmtypes.LoadMessagesAsLLMMessages(llmChatID, chatStore)
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

	sseWriter := sse.NewSSEWriter(w)

	timeQueryToolImp := toolimp.MakeTimeQueryTool(lang)
	toolsImp := []llm.ToolIMP{timeQueryToolImp}

	if req.WebSearchEnabled {
		webSearcher := sessionWebSearcher(sess)
		webSearchToolImp := toolimp.MakeWebSearchTool(r.Context(), webSearcher, lang)
		toolsImp = append(toolsImp, webSearchToolImp)
	}

	if req.TraitSearchEnabled {
		traitsStore, terr := h.openBrainDB(sess)
		if terr != nil {
			h.logger.Errorf("failed to open traits store: %v", terr)
		}
		if traitsStore != nil {
			defer h.closeBrainDB(traitsStore)
		}

		embedder := sessionEmbedder(sess)
		embedderSetting := sessionEmbedderApiSetting(sess)
		traitSearcher := &traitSearchAdapter{
			client:     embedder,
			store:      traitsStore,
			lang:       lang,
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
		persistMessageToDB(sess, assistantMsg, msgChatID, chatStore)
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
