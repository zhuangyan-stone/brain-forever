package agent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
)

// ============================================================
// ChatHandler — POST /api/chat handler (core)
// ============================================================

// ChatAgent handles chat requests, integrating RAG retrieval + LLM streaming
//
// ChatAgent uses SessionManager to isolate each user's chat history by sessionID.
// The frontend only needs to send the user's latest message each time,
// and ChatAgent merges history with new messages before sending to the actual LLM.
type ChatAgent struct {
	traitSearcher toolimp.TraitSearcher // Personal knowledge base (RAG) search
	webSearcher   toolimp.WebSearcher   // Web search interface

	charLLMClient llm.Client // LLM API client for chat

	sessionManager *SessionManager
	cookieName     string // cookie name for reading/writing sessionID

	// defaultLang is the default language for i18n (e.g., "zh-CN", "en").
	// Used for translating system prompts, tool descriptions, and other
	// content sent to the AI API and frontend.
	defaultLang string
}

// LLMInfo is the response for the LLM info endpoint.
type LLMInfo struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Website string `json:"website"`
}

// OnGetLLMInfo handles GET /api/chat/info/llm requests.
// Returns the current chat LLM provider name, model name, and official website URL as JSON.
func (h *ChatAgent) OnGetLLMInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LLMInfo{
		Name:    h.charLLMClient.Name(),
		Model:   h.charLLMClient.Model(),
		Website: h.charLLMClient.Website(),
	})
}

// Close releases underlying resources held by the ChatHandler.
// Currently closes the VectorStore (knowledge base) database.
func (h *ChatAgent) Close() error {
	return h.traitSearcher.Close()
}

// NewChatHandler creates a ChatHandler
//
// cookieName: the cookie name for reading/writing sessionID, e.g. "brain_go_session"
// defaultLang: the default language for i18n, e.g. "zh-CN", "en". Empty string defaults to "en".
func NewChatHandler(
	traitSearcher toolimp.TraitSearcher,
	webSearcher toolimp.WebSearcher,
	chatLLMClient llm.Client,
	cookieName string,
	defaultLang string,
) *ChatAgent {
	if defaultLang == "" {
		defaultLang = "en"
	}
	return &ChatAgent{
		traitSearcher:  traitSearcher,
		webSearcher:    webSearcher,
		charLLMClient:  chatLLMClient,
		sessionManager: NewSessionManager(),
		cookieName:     cookieName,
		defaultLang:    defaultLang,
	}
}

func makeAssistantBrokenMessage(lang string, id int64) Message {
	brokenMsg := i18n.TL(lang, "assistant_broken_message")

	return Message{
		ID:        id,
		Role:      llm.RoleAssistant,
		Content:   brokenMsg,
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// generateSessionSN generates a unique serial number for a chat session.
// Format: chat-<16randomHex>-<timestamp>
func generateSessionSN() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("chat-fallback-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("chat-%x-%d", b, time.Now().UnixNano())
}

// ensureDBSession ensures that the current chat has a corresponding record
// in the chat_sessions table. If dbSessionID is 0, it creates a new session
// record and sets dbSessionID.
//
// Exception: anonymous users (chatStore == nil) have no DB persistence,
// so no DB session record is created.
// Must be called with session.mu held.
func ensureDBSession(session *session) {
	if session.chatStore == nil {
		return // Anonymous user, no DB persistence
	}
	if session.currentChat.dbSessionID != 0 {
		return // Already has a DB session
	}

	sn := generateSessionSN()
	title := session.currentChat.title

	dbSess, err := session.chatStore.InsertSession(sn, 0, title, 0)
	if err != nil {
		log.Printf("failed to insert DB session for user %s: %v", session.userNo, err)
		return
	}

	session.currentChat.dbSessionID = dbSess.ID
}

// persistMessageToDB inserts a single message into the chat_messages table.
//
// Exception: anonymous users (chatStore == nil) have no DB persistence,
// so messages are not stored in the database.
// Must be called with session.mu held.
func persistMessageToDB(session *session, msg *Message) {
	if session.chatStore == nil {
		return // Anonymous user, no DB persistence
	}
	if session.currentChat.dbSessionID == 0 {
		log.Printf("cannot persist message: no DB session ID for user %s", session.userNo)
		return
	}

	// Map agent.Message role to store.Message role: 0=user, 1=assistant
	var role int
	switch msg.Role {
	case llm.RoleUser:
		role = 0
	case llm.RoleAssistant:
		role = 1
	default:
		return // Skip system messages
	}

	// Map agent.Message.ID (group index) to store.Message.GroupIndex
	groupIndex := int(msg.ID)

	// Map agent.Message.Reasoning to store.Message.Reasoning (*string)
	var reasoning *string
	if msg.Reasoning != "" {
		reasoning = &msg.Reasoning
	}

	if err := session.chatStore.InsertMessage(
		session.currentChat.dbSessionID,
		groupIndex,
		role,
		msg.Content,
		reasoning,
	); err != nil {
		log.Printf("failed to persist message to DB for user %s: %v", session.userNo, err)
	}
}

// Enqueue a new message for request, assign an ID
func appendNewRequestMessage(session *session, reqMsg *Message, lang string) {
	if reqMsg.ID != 0 {
		panic(fmt.Sprintf("new request message's ID is not 0, but %d", reqMsg.ID))
	}

	var lastID int64 = 0

	// Assign new ID if ID==0 (frontend no longer manages IDs)
	if len(session.currentChat.history) > 0 {
		lastMsg := session.currentChat.history[len(session.currentChat.history)-1]
		lastID = lastMsg.ID

		// Also check if the last message is a user message!
		// When the AI is interrupted mid-thought or mid-response, we won't get an assistant message,
		// so the last message will be a user message.
		// In this case, we need to manually append an assistant message.
		if lastMsg.Role == llm.RoleUser {
			assistantMsg := makeAssistantBrokenMessage(lang, lastID+1)
			session.currentChat.history = append(session.currentChat.history, assistantMsg)

			// Also persist the broken assistant message to DB if logged in
			persistMessageToDB(session, &assistantMsg)
		}
	}

	reqMsg.ID = lastID + 1

	// Append to history
	session.currentChat.history = append(session.currentChat.history, *reqMsg)

	// Ensure a DB session record exists for logged-in users
	ensureDBSession(session)

	// Persist the user message to DB if logged in
	persistMessageToDB(session, reqMsg)
}

// Enqueue a new message for response (message's ID must != 0)
func appendNewResponseMessage(session *session, resMsg *Message) {
	if resMsg.ID == 0 {
		panic("new response message's ID is 0")
	}

	session.currentChat.history = append(session.currentChat.history, *resMsg)

	// Persist the assistant message to DB if logged in
	persistMessageToDB(session, resMsg)
}

// toRawMessages converts agent.Message slice to llm.Message slice.
func toRawMessages(msgs []Message) []llm.Message {
	result := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		result[i] = llm.Message{Role: m.Role, Content: m.Content}
	}
	return result
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
	defer session.mu.Unlock()

	// 3. Determine the language for this request.
	// Priority: request header Accept-Language > handler defaultLang > "en"
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// 4. Add the message to the history and assign it a unique ID
	appendNewRequestMessage(session, &req.Message, lang)

	if req.Message.ID <= 0 {
		panic("new message's ID is zero still after append to history")
	}

	// 5. Create SSE writer
	sseWriter := sse.NewSSEWriter(w)

	// Convert agent.Message history to llm.Message for the API call
	llmMsgs := toRawMessages(session.currentChat.history)

	startSystemMsg := llm.Message{
		Role:    llm.RoleSystem,
		Content: makeFixSystemPromptContent(lang),
	}
	messages := make([]llm.Message, 0, 1+len(llmMsgs))
	messages = append(messages, startSystemMsg)
	messages = append(messages, llmMsgs...)

	// 6. Build tool definitions with translated descriptions.
	// time_query tool is always available.
	// web_search tool is only provided when WebSearchEnabled is true.
	timeQueryToolImp := toolimp.MakeTimeQueryTool(lang)
	toolsImp := []llm.ToolIMP{timeQueryToolImp}

	if req.WebSearchEnabled {
		webSearchToolImp := toolimp.MakeWebSearchTool(r.Context(), h.webSearcher, lang)
		toolsImp = append(toolsImp, webSearchToolImp)
	}

	// 7. Stream with tool call handling
	h.callLLMWithPipeline(r.Context(), session, sseWriter,
		req.Message.ID,
		messages,
		toolsImp,
		req.DeepThink,
		lang)
}

// ============================================================
// Helper functions
// ============================================================

// makeFixSystemPromptContent returns the system prompt content string,
// translated according to the given language.
func makeFixSystemPromptContent(lang string) string {
	return i18n.SystemPrompt.TL(lang, "chat")
}
