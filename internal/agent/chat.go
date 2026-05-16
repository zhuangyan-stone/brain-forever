package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

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
	charLLMClient llm.LLMClient         // LLM API client

	sessionManager *SessionManager
	cookieName     string // cookie name for reading/writing sessionID

	// defaultLang is the default language for i18n (e.g., "zh-CN", "en").
	// Used for translating system prompts, tool descriptions, and other
	// content sent to the AI API and frontend.
	defaultLang string
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
func NewChatHandler(traitSearcher toolimp.TraitSearcher, webSearcher toolimp.WebSearcher, llmClient llm.LLMClient, cookieName string, defaultLang string) *ChatAgent {
	if defaultLang == "" {
		defaultLang = "en"
	}
	return &ChatAgent{
		traitSearcher:  traitSearcher,
		webSearcher:    webSearcher,
		charLLMClient:  llmClient,
		sessionManager: NewSessionManager(),
		cookieName:     cookieName,
		defaultLang:    defaultLang,
	}
}

// Enqueue a new message for request, assign an ID
func appendNewRequestMessage(session *session, reqMsg *Message) {
	if reqMsg.ID != 0 {
		panic(fmt.Sprintf("new request message's ID is not 0, but %d", reqMsg.ID))
	}

	// Assign new ID if ID==0 (frontend no longer manages IDs)
	if len(session.history) > 0 {
		reqMsg.ID = session.history[len(session.history)-1].ID + 1
	} else {
		reqMsg.ID = 1
	}

	// Append to history
	session.history = append(session.history, *reqMsg)
}

// Enqueue a new message for response (message's ID must != 0)
func appendNewResponseMessage(session *session, resMsg *Message) {
	if resMsg.ID == 0 {
		panic("new response message's ID is 0")
	}

	session.history = append(session.history, *resMsg)
}

// toRawMessages converts agent.Message slice to llm.Message slice.
func toRawMessages(msgs []Message) []llm.Message {
	result := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case "system":
			result[i] = llm.Message{Role: "system", Content: m.Content}
		case "user":
			result[i] = llm.Message{Role: "user", Content: m.Content}
		case "assistant":
			result[i] = llm.Message{Role: "assistant", Content: m.Content}
		case "tool":
			result[i] = llm.Message{Role: "tool", Content: m.Content}
		default:
			result[i] = llm.Message{Role: "user", Content: m.Content}
		}
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

// OnGetSessionTitle handles GET /api/session/title requests.
// It reads the "title" query parameter as the original title,
// takes the first 5 messages from the session history,
// sends them to the LLM to generate a new concise title,
// and returns the new title as JSON. On error or empty LLM response, returns the original title.
// The generated title is also saved to session.Title so that subsequent page refreshes
// (OnRestoreSession) will use the saved title instead of re-deriving it.
// The title_state is also returned to indicate the title modification state:
//
//	0: original title, 1: AI-modified title, 2: user-modified title.
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
	history := make([]Message, 0, len(session.history))
	history = append(history, session.history...)
	session.mu.Unlock()

	// Take at most the first 6 messages
	const maxMsgs = 6
	if len(history) > maxMsgs {
		history = history[:maxMsgs]
	}

	// Build the LLM prompt with i18n support
	systemPrompt := i18n.TL(lang, "session_title_system_prompt")
	var contentBuilder strings.Builder

	for _, msg := range history {
		switch msg.Role {
		case "user":
			contentBuilder.WriteString("A: ")
		case "assistant":
			contentBuilder.WriteString("B: ")
		}
		contentBuilder.WriteString(msg.Content)
		contentBuilder.WriteString("\n")
	}

	messages := make([]llm.Message, 0, 2)
	messages = append(messages, llm.Message{Role: "system", Content: systemPrompt})
	messages = append(messages, llm.Message{Role: "user", Content: contentBuilder.String()})

	newTitle := ""
	titleChanged := false

	// Call LLM (non-streaming)
	resp, err := h.charLLMClient.Chat(r.Context(), messages)

	if err != nil {
		log.Printf("Make char-llm client fail. %v", err)
	} else if len(resp.Choices) > 0 {
		// Extract the reply content
		newTitle = resp.Choices[0].Message.Content
	}

	// Validate the generated title:
	// - If LLM returned empty content, fall back to original title
	// - If the generated title is unreasonably long (>50 chars), the LLM likely
	//   failed to generate a concise title; discard it and use the original title instead.
	const maxTitleLen = 50
	if newTitle == "" || len([]rune(newTitle)) > maxTitleLen {
		newTitle = originalTitle
	}

	// Only update session title and state if the new title differs from the original
	if newTitle != originalTitle {
		session.SetTitle(newTitle)
		// State can only move forward: 0→1 (AI-modified)
		session.SetTitleState(TitleStateAIModified)
		titleChanged = true
	}

	// Return the new title as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"title":   newTitle,
		"changed": titleChanged,
	})
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

	// 3. Add the message to the history and assign it a unique ID
	appendNewRequestMessage(session, &req.Message)

	if req.Message.ID <= 0 {
		panic("new message's ID is zero still after append to history")
	}

	// 4. Determine the language for this request.
	// Priority: request header Accept-Language > handler defaultLang > "en"
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// 5. Create SSE writer
	sseWriter := sse.NewSSEWriter(w)

	// Convert agent.Message history to llm.Message for the API call
	llmMsgs := toRawMessages(session.history)

	startSystemMsg := llm.Message{
		Role:    "system",
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
	return i18n.TL(lang, "system_prompt")
}
