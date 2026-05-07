package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"

	"BrainOnline/infra/3rdapi/llm"
	"BrainOnline/infra/sse"
)

// ============================================================
// ChatHandler — POST /api/chat handler (core)
// ============================================================

// ChatHandler handles chat requests, integrating RAG retrieval + LLM streaming
//
// ChatHandler uses SessionManager to isolate each user's chat history by sessionID.
// The frontend only needs to send the user's latest message each time,
// and ChatHandler merges history with new messages before sending to the actual LLM.
type ChatHandler struct {
	traitSearcher TraitSearcher // Personal knowledge base (RAG) search
	webSearcher   WebSearcher   // Web search interface
	aiClient      *llm.OpenAICompatibleClient

	sessionManager *SessionManager
	cookieName     string // cookie name for reading/writing sessionID
}

// NewChatHandler creates a ChatHandler
//
// cookieName: the cookie name for reading/writing sessionID, e.g. "brain_go_session"
func NewChatHandler(traitSearcher TraitSearcher, webSearcher WebSearcher, aiClient *llm.OpenAICompatibleClient, cookieName string) *ChatHandler {
	return &ChatHandler{
		traitSearcher:  traitSearcher,
		webSearcher:    webSearcher,
		aiClient:       aiClient,
		sessionManager: NewSessionManager(),
		cookieName:     cookieName,
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

// toOpenAIMessages converts agent.Message slice to openai.ChatCompletionMessageParamUnion slice.
func toOpenAIMessages(msgs []Message) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case "system":
			result[i] = openai.SystemMessage(m.Content)
		case "user":
			result[i] = openai.UserMessage(m.Content)
		case "assistant":
			result[i] = openai.AssistantMessage(m.Content)
		case "tool":
			result[i] = openai.ToolMessage(m.Content, "")
		default:
			result[i] = openai.UserMessage(m.Content)
		}
	}
	return result
}

// resolveNewMessageRequest parses and validates the incoming chat request.
// Returns nil if validation fails (the caller should return immediately in that case).
func (h *ChatHandler) resolveNewMessageRequest(w http.ResponseWriter, r *http.Request) *ChatRequest {
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
func (h *ChatHandler) OnNewMessage(w http.ResponseWriter, r *http.Request) {
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

	// 3. Assign ID and append the new message from the frontend
	appendToMessageHistory(session, &req.Message)

	if req.Message.ID <= 0 {
		panic("new message's ID is zero still after append to history")
	}

	// 4. Create SSE writer
	sseWriter := sse.NewSSEWriter(w)

	// Convert agent.Message history to openai.ChatCompletionMessageParamUnion for the API call
	llmMsgs := toOpenAIMessages(session.history)

	systemMsg := makeFixSystemPrompt()
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(llmMsgs)+1)
	messages = append(messages, systemMsg)
	messages = append(messages, llmMsgs...)

	// 7. Stream with tool call handling (web_search tool is always available)
	toolDef := webSearchToolDefinition()
	fullReply, webPages, err := h.performLLMStreamingCall(r.Context(), sseWriter, messages, []openai.ChatCompletionToolUnionParam{toolDef})
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

	// 8. Determine token counts: prefer the API's real usage info,
	//     fall back to manual estimation if the provider doesn't support it.
	isEstimated := true
	var promptTokens, completionTokens int = -1, -1

	if usage := h.aiClient.GetUsageInfo(); usage != nil {
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
			isEstimated = false
		}

		if usage.PromptTokens > 0 {
			promptTokens = int(usage.PromptTokens)
		}
		if usage.CompletionTokens > 0 {
			completionTokens = int(usage.CompletionTokens)
		}
	}

	// Fall back to estimation for any token count the API didn't provide
	if promptTokens == -1 {
		var content strings.Builder
		for _, msg := range messages {
			if c := msg.GetContent(); c.AsAny() != nil {
				if s, ok := c.AsAny().(*string); ok && s != nil {
					content.WriteString(*s)
				}
			}
		}
		promptTokens = h.aiClient.EstimateTokens(content.String())
	}

	if completionTokens == -1 {
		completionTokens = h.aiClient.EstimateTokens(fullReply)
	}

	usage := &Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		IsEstimated:      isEstimated,
	}

	// 9. Append the LLM's full reply to the user's internal history
	//     The AI reply reuses the user message's ID (source ID)
	if len(fullReply) > 0 {
		session.history = append(session.history, Message{
			ID:        req.Message.ID, // same as user message's id
			Role:      "assistant",
			Content:   fullReply,
			CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			Usage:     usage,
		})
	}

	// 10. Send done event
	sseWriter.WriteEvent(SSEEvent{
		Type:  "done",
		Usage: usage,
		MsgID: req.Message.ID,
	})
}

// ============================================================
// Helper functions
// ============================================================

// makeFixSystemPrompt creates a system message with the fixed AI assistant prompt
// (including search suggestion behavior instructions).
func makeFixSystemPrompt() openai.ChatCompletionMessageParamUnion {
	now := time.Now().Format(time.DateTime)

	return openai.SystemMessage(fmt.Sprintf(`You are an AI assistant, and also one who, during conversations with users, faithfully records various user characteristics, 
deepens understanding of the user, and gradually builds a user profile to better provide service.
When necessary, you will call relevant tools to obtain the information needed to better complete 
your responses. Currently, there are two tools available: user traits and web information.

Some real-time information for your reference in responses:
Current time: %s`, now))
}
