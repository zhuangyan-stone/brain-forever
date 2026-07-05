package agent

import (
	"fmt"
	"time"

	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
)

// ============================================================
// Request / Response type definitions
// ============================================================

// Message represents a chat message used by the agent layer.
// It extends the OpenAI-compatible Message with fields needed
// for frontend-backend sync and session management.
type Message struct {
	ID      int64  `json:"id"`              // Unique message ID for frontend-backend sync
	Role    string `json:"role"`            // user | assistant | system
	Content string `json:"content"`         // Message content
	Usage   *Usage `json:"usage,omitempty"` // Token usage (nil for user messages)

	// Reasoning holds the deep thinking / reasoning chain content associated
	// with this assistant message. Populated when deep_think is enabled.
	// Used by the frontend to restore the reasoning area after page refresh.
	Reasoning string `json:"reasoning,omitempty"`

	// Sources holds web search result references associated with this message.
	// Populated for assistant messages that involved web search.
	// Used by the frontend to restore the sources-panel after page refresh.
	Sources []toolimp.WebSource `json:"sources,omitempty"`

	CreatedAt time.Time `json:"created_at"` // UTC time, e.g. "2026-05-02T16:30:00Z"

	// Interrupted indicates the message interruption state:
	//   0 = done (normal completion)
	//   1 = user-interrupted (user clicked stop mid-stream)
	//   2 = backend-error (LLM/API error, message incomplete)
	Interrupted int `json:"interrupted"`
}

// ChatRequest is the chat request sent from the frontend
type ChatRequest struct {
	Message            Message `json:"message"`
	Stream             bool    `json:"stream"` // Always true
	DeepThink          bool    `json:"deep_think"`
	WebSearchEnabled   bool    `json:"web_search_enabled"`
	TraitSearchEnabled bool    `json:"trait_search_enabled"`
	FrontSN            string  `json:"front_sn"` // Frontend-generated temporary SN for new chats
}

// ============================================================
// SSE event types (business-specific, used by ChatHandler)
//
// Each event type has its own struct to avoid the "fat" struct pattern,
// ensuring only the fields relevant to each event are serialized.
// ============================================================

// ReasoningEvent is sent when the LLM produces reasoning content.
type ReasoningEvent struct {
	Type    string `json:"type"`              // "reasoning"
	Subject string `json:"subject,omitempty"` // "" or "tool-pending"
	Tool    string `json:"tool,omitempty"`    // tool name (for tool-pending)
	Content string `json:"content,omitempty"`
}

// ReasoningEndEvent signals the end of the reasoning phase.
type ReasoningEndEvent struct {
	Type string `json:"type"` // "reasoning_end"
}

// TextEvent carries incremental text content from the LLM.
type TextEvent struct {
	Type    string `json:"type"` // "text"
	Content string `json:"content,omitempty"`
}

// WebSourceEvent carries web search sources.
type WebSourceEvent struct {
	Type       string              `json:"type"` // "web_source"
	WebSources []toolimp.WebSource `json:"web_sources,omitempty"`
}

// DoneEvent signals that the LLM response is complete.
type DoneEvent struct {
	Type      string `json:"type"` // "done"
	Usage     *Usage `json:"usage,omitempty"`
	MsgID     int64  `json:"msg_id,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// ErrorEvent is sent when an error occurs during streaming.
type ErrorEvent struct {
	Type    string `json:"type"` // "error"
	Message string `json:"message,omitempty"`
}

// ChatCreatedEvent is sent when a new chat session is created in the DB.
type ChatCreatedEvent struct {
	Type    string `json:"type"` // "chat_created"
	SN      string `json:"sn,omitempty"`
	FrontSN string `json:"front_sn,omitempty"`
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	IsEstimated      bool `json:"is_estimated"` // true if any of the token counts was estimated client-side (not from the LLM API)
}

// ============================================================
// DB -> Agent message conversion helpers
// ============================================================

// convertDBMessagesToAgentMessages converts store.Message slice to agent.Message slice,
// loading associated WebSources from DB matched by group_index.
//
// WebSources are stored in the independent web_sources table (not a chat_messages column).
// persistMessageToDB persists Sources synchronously when inserting a message.
// During conversion, ListWebSourcesByChat is called to load all web_sources for the chat,
// then matched to each message by msg_id (= group_index).
//
// chatStore and chatID are used to query the web_sources table; if chatStore is nil or
// chatID is 0, Sources remain empty (compatible with anonymous users and other no-DB scenarios).
// Returns an error if loading web sources fails.
func convertDBMessagesToAgentMessages(dbMessages []store.Message, chatStore *store.ChatStore, chatID int64) ([]Message, error) {
	// Load web sources for this chat (if available)
	var sourcesByMsgID map[int64][]store.WebSource
	if chatStore != nil && chatID != 0 {
		var err error
		sourcesByMsgID, err = chatStore.ListWebSourcesByChat(chatID)
		if err != nil {
			return nil, fmt.Errorf("failed to list web sources for chat %d: %w", chatID, err)
		}
	}

	msgs := make([]Message, 0, len(dbMessages))
	for _, m := range dbMessages {
		role := llm.RoleUser
		if m.Role == 1 {
			role = llm.RoleAssistant
		}
		agentMsg := Message{
			ID:          int64(m.GroupIndex),
			Role:        role,
			Content:     m.Content,
			CreatedAt:   m.CreateAt,
			Interrupted: m.Interrupted,
		}
		if m.Reasoning != nil {
			agentMsg.Reasoning = *m.Reasoning
		}

		// Attach web sources if available for this message group
		if sourcesByMsgID != nil {
			if sources, ok := sourcesByMsgID[int64(m.GroupIndex)]; ok && len(sources) > 0 {
				agentMsg.Sources = make([]toolimp.WebSource, 0, len(sources))
				for _, src := range sources {
					agentMsg.Sources = append(agentMsg.Sources, toolimp.WebSource{
						Title:       src.Title,
						Content:     src.Content,
						URL:         src.URL,
						SiteName:    src.SiteName,
						SiteIcon:    src.SiteIcon,
						PublishDate: src.PublishDate,
						Score:       src.Score,
					})
				}
			}
		}

		msgs = append(msgs, agentMsg)
	}
	return msgs, nil
}

// loadMessagesAsLLMMessages loads messages from DB via the given chatStore
// and converts to llm.Message slice.
// Caller must hold session.mu.
func loadMessagesAsLLMMessages(s *session, chatStore *store.ChatStore) ([]llm.Message, error) {
	if s.user.currentChat.dbChat == nil {
		return nil, fmt.Errorf("no DB session")
	}
	chatID := s.user.currentChat.dbChat.ID
	if chatID == 0 {
		return nil, fmt.Errorf("no DB session")
	}
	dbMessages, err := chatStore.ListMessages(chatID)
	if err != nil {
		return nil, err
	}
	result := make([]llm.Message, 0, len(dbMessages))
	for _, m := range dbMessages {
		role := llm.RoleUser
		if m.Role == 1 {
			role = llm.RoleAssistant
		}
		result = append(result, llm.Message{Role: role, Content: m.Content})
	}
	return result, nil
}

// ensureAssistantForOrphanUser checks if the last message is an orphan user message
// (user message without a corresponding assistant reply), and if so, appends a
// broken assistant message.
//
// Scenario: AI is interrupted during reply (backend crash, interrupt, etc.),
// leaving only the user message in DB.
// This compensation ensures broken messages display correctly after page refresh.
func ensureAssistantForOrphanUser(msgs []Message, lang string) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	lastMsg := msgs[len(msgs)-1]
	if lastMsg.Role == llm.RoleUser {
		brokenMsg := makeAssistantBrokenMessage(lang, lastMsg.ID)
		brokenMsg.Interrupted = 2 // backend-error
		msgs = append(msgs, brokenMsg)
	}
	return msgs
}
