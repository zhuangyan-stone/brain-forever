package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"BrainOnline/i18n"
	"BrainOnline/infra/httpx/sse"
	"BrainOnline/infra/llm"
	"BrainOnline/internal/agent/toolcalls"
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
	traitSearcher toolcalls.TraitSearcher // Personal knowledge base (RAG) search
	webSearcher   toolcalls.WebSearcher   // Web search interface
	charLLMClient llm.LLMClient           // LLM API client

	sessionManager *SessionManager
	cookieName     string // cookie name for reading/writing sessionID

	// defaultLang is the default language for i18n (e.g., "zh-CN", "en").
	// Used for translating system prompts, tool descriptions, and other
	// content sent to the AI API and frontend.
	defaultLang string

	// webPagesCollector collects web search page results during a streaming LLM call.
	// It is set before performLLMStreamingCall and read after the call returns to send
	// web sources to the frontend.
	webPagesCollector *[]toolcalls.WebSource
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
func NewChatHandler(traitSearcher toolcalls.TraitSearcher, webSearcher toolcalls.WebSearcher, llmClient llm.LLMClient, cookieName string, defaultLang string) *ChatAgent {
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

// Enqueue a new message, assign an ID, and return a snapshot of all messages
func appendToMessageHistory(session *session, newMsg *Message) {
	if newMsg.ID != 0 {
		panic(fmt.Sprintf("new message's ID is not 0, but %d", newMsg.ID))
	}

	// Assign new ID if ID==0 (frontend no longer manages IDs)
	if len(session.history) > 0 {
		newMsg.ID = session.history[len(session.history)-1].ID + 1
	} else {
		newMsg.ID = 1
	}

	// Append to history
	session.history = append(session.history, *newMsg)
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
	appendToMessageHistory(session, &req.Message)

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
	messages := make([]llm.Message, 0, 1+len(llmMsgs)+1)
	messages = append(messages, startSystemMsg)
	messages = append(messages, llmMsgs...)

	// 6. Build tool definition with translated description
	toolDef := webSearchToolDefinitionRaw(lang)

	// 7. Stream with tool call handling (web_search tool is always available)
	fullReply, webPages, err := h.performLLMStreamingCall(r.Context(), sseWriter, messages, []llm.ToolDefinition{toolDef})
	if err != nil {
		sseWriter.WriteEvent(SSEEvent{
			Type:    "error",
			Message: fmt.Sprintf("%v", err),
		})
		return
	}

	// Send web search sources event (if any)
	if len(webPages) > 0 {
		if err := sseWriter.WriteEvent(SSEEvent{
			Type:       "sources",
			WebSources: webPages,
		}); err != nil {
			log.Printf("failed to write web sources event: %v", err)
		}
	}

	// 7. Determine token counts from the LLM client's usage info
	isEstimated := true
	var promptTokens, completionTokens int = -1, -1

	if usage := h.charLLMClient.GetUsageInfo(); usage != nil {
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
			isEstimated = false
		}

		if usage.PromptTokens > 0 {
			promptTokens = usage.PromptTokens
		}
		if usage.CompletionTokens > 0 {
			completionTokens = usage.CompletionTokens
		}
	}

	// If the API didn't provide token counts, use simple estimation
	if promptTokens == -1 {
		var content strings.Builder
		for _, msg := range messages {
			content.WriteString(msg.Content)
		}
		promptTokens = len(content.String()) / 4
		if promptTokens < 1 {
			promptTokens = 1
		}
	}

	if completionTokens == -1 {
		completionTokens = len(fullReply) / 4
		if completionTokens < 1 {
			completionTokens = 1
		}
	}

	usage := &Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		IsEstimated:      isEstimated,
	}

	// 8. Append the LLM's full reply to the user's internal history
	//     The AI reply reuses the user message's ID (source ID)
	if len(fullReply) > 0 {
		assistantMsg := Message{
			ID:        req.Message.ID, // same as user message's id
			Role:      "assistant",
			Content:   fullReply,
			CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			Usage:     usage,
		}
		// Attach web search sources so they can be restored after page refresh
		if len(webPages) > 0 {
			assistantMsg.Sources = webPages
		}
		session.history = append(session.history, assistantMsg)
	}

	// 9. Send done event
	sseWriter.WriteEvent(SSEEvent{
		Type:  "done",
		Usage: usage,
		MsgID: req.Message.ID,
	})
}

// ============================================================
// Helper functions
// ============================================================

// makeFixSystemPromptContent returns the system prompt content string,
// translated according to the given language.
func makeFixSystemPromptContent(lang string) string {
	return i18n.TL(lang, "system_prompt")
}

// webSearchToolDefinitionRaw returns the ToolDefinition for web search
// using llm types, with translated descriptions.
func webSearchToolDefinitionRaw(lang string) llm.ToolDefinition {
	// Build the schema as a Go map and marshal it to JSON.
	// Using json.Marshal ensures the description string is properly escaped
	// (e.g., double quotes, newlines, etc.), so any translation content is safe.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"search_queries": map[string]any{
				"type":        "string",
				"description": i18n.TL(lang, "web_search_param_description"),
			},
		},
		"required":             []string{"search_queries"},
		"additionalProperties": false,
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal web search tool schema: %v", err))
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(schemaBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to parse web search tool schema: %v", err))
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        toolcalls.WebSearchToolName,
			Description: i18n.TL(lang, "web_search_tool_description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}
