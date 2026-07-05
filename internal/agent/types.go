package agent

import (
	"fmt"
	"sync"
	"time"

	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
	"BrainForever/internal/store/dbcfg"
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
// Session management -isolates user chat messages by sessionID
// ============================================================

// TitleState represents the state of the session title modification.
//
//	0: original title (default, "New Chat" for new sessions)
//	1: AI-modified title
//	2: user-modified title
type TitleState int

const (
	TitleStateOriginal     TitleState = iota // 0: original title
	TitleStateAIModified                     // 1: AI-modified title
	TitleStateUserModified                   // 2: user-modified title
)

type chat struct {
	dbChat *store.Chat // Bridge to store.Chat (never nil after creation)

	title      string     // Session title, generated from the first user message content
	titleState TitleState // Title modification state
}

// session represents an individual user's session
type session struct {
	mu      sync.Mutex // protects: currentChat, userSN, lastActivity
	chatsMu sync.Mutex // protects: chats

	lastActivity time.Time // Last activity time, used by GC for cleanup

	id          string       // HTTP cookie session ID
	currentChat *chat        // Current active chat (messages, title, titleState)
	chats       []store.Chat // User's chat list from the database
	userSN      string       // User serial number; empty = not logged in
}

// GetTitle returns the current title and its modification state atomically.
func (s *session) GetTitle() (string, TitleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentChat.title, s.currentChat.titleState
}

// SetTitle sets both the title and its modification state atomically.
// Title is always updated. TitleState only moves forward (0->, 0->, 1->).
func (s *session) SetTitle(newTitle string, newState TitleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentChat.title = newTitle
	if newState > s.currentChat.titleState {
		s.currentChat.titleState = newState
	}
}

// switchToUser sets the session's user state.
// For login: sn is non-empty, chats are pre-loaded by UserStore.Login().
// For logout: sn is empty, chats is nil (clears session).
func (s *session) switchToUser(sn string, chats []store.Chat) {
	if chats == nil {
		chats = []store.Chat{}
	}
	s.chatsMu.Lock()
	s.chats = chats
	s.chatsMu.Unlock()

	s.mu.Lock()
	s.currentChat = &chat{}
	s.userSN = sn
	s.mu.Unlock()
}

// switchToChat switches the current active chat to a historical session
// identified by its serial number (SN). It only sets dbChat without loading
// messages into memory. Messages are loaded from DB on demand.
// Returns an error if the session is not found.
func (s *session) switchToChat(sn string) error {
	// Phase 1: Find the chat by SN (internally locks chatsMu)
	foundChat := s.findChatBySN(sn)
	if foundChat == nil {
		return fmt.Errorf("session not found: %s", sn)
	}

	// Phase 2: Set as current chat under mu lock (no messages loaded)
	s.mu.Lock()
	s.currentChat = &chat{
		dbChat:     foundChat,
		title:      foundChat.Title,
		titleState: TitleState(foundChat.TitleState),
	}
	s.mu.Unlock()

	return nil
}

// findChatBySN finds a chat by its serial number (SN) in the session's chat list.
// It locks chatsMu internally, so the caller does not need to hold it.
// Returns nil for the chat pointer if not found.
// NOTE: The returned chat pointer points into the internal slice and should
// not be modified. The caller must open a ChatStore separately for DB operations.
func (s *session) findChatBySN(sn string) *store.Chat {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()

	for i := range s.chats {
		if s.chats[i].SN == sn {
			return &s.chats[i]
		}
	}
	return nil
}

// SessionManager manages all user sessions.
// It does not hold any database connections; databases are accessed
// via the global dbcfg package or theUserStore singleton.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*session
}

// Close releases all sessions. No database stores to close since
// they are opened on-demand and closed after use.
func (sm *SessionManager) Close() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Clear all sessions. No long-lived DB stores to close.
	sm.sessions = make(map[string]*session)
}

// NewSessionManager creates a SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*session),
	}
}

// GetOrCreate gets or creates a session for the given sessionID.
// No database stores are opened at creation time.
func (sm *SessionManager) GetOrCreate(sessionID string) *session {
	sm.mu.RLock()
	s, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if ok {
		s.mu.Lock()
		s.lastActivity = time.Now()
		s.mu.Unlock()
		return s
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check
	if s, ok := sm.sessions[sessionID]; ok {
		s.mu.Lock()
		s.lastActivity = time.Now()
		s.mu.Unlock()
		return s
	}

	s = &session{
		id:           sessionID,
		lastActivity: time.Now(),
		userSN:       "",
		currentChat:  &chat{},
	}

	sm.sessions[sessionID] = s
	return s
}

// Remove removes the session for the given sessionID (optional)
func (sm *SessionManager) Remove(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, sessionID)
}

// DeleteMessage deletes a user message and all associated messages (AI reply, etc.)
// that share the same source ID. It finds the first message with the given msgID,
// then removes all consecutive messages with the same ID. Stops at the first message
// with a different ID. Returns an error if the msgID is not found.
func (sm *SessionManager) DeleteMessage(sessionID string, msgID int64) error {
	sm.mu.RLock()
	s, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session not found")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = time.Now()

	if s.currentChat.dbChat == nil {
		return fmt.Errorf("no DB session")
	}
	chatID := s.currentChat.dbChat.ID
	if chatID == 0 {
		return fmt.Errorf("no DB session")
	}

	// Open chat store on-demand via dbcfg, delete, close
	chatStore, err := dbcfg.OpenLocalChatDB(s.userSN)
	if err != nil {
		return fmt.Errorf("failed to open chat store: %w", err)
	}
	defer dbcfg.CloseLocalChatDB(chatStore)

	// Delete messages and their web sources from DB
	return chatStore.DeleteMessageGroup(chatID, int(msgID))
}

// isBlankChat checks whether currentChat is a "blank chat" -
// a new chat that has NOT yet been added to session.chats[] and has no SN.
//
// A blank chat is created by OnNewChat (PUT /api/chat/new) when the user
// starts a new conversation. It has no SN, no DB record, and is NOT in session.chats[].
// The SN is only generated later when ensureDBSession is called (on first message).
//
// Detection: a blank chat has dbChat == nil or dbChat.SN == "".
// A historical chat (switched from session.chats[]) always has a non-empty SN.
//
// Must be called with session.mu held.
func (s *session) isBlankChat() bool {
	return s.currentChat == nil || s.currentChat.dbChat == nil || s.currentChat.dbChat.SN == ""
}

// addChatToList adds a store.Chat to the in-memory chat list (session.chats)
// if it's not already present. Must be called with session.mu NOT held
// (it locks chatsMu internally).
// This is called from ensureDBSession after creating a new DB chat record,
// so that the new chat immediately appears in the left sidebar's chat list.
func (s *session) addChatToList(chat store.Chat) {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()

	// Avoid duplicates
	for _, c := range s.chats {
		if c.SN == chat.SN {
			return
		}
	}

	// Prepend to list (newest first)
	s.chats = append([]store.Chat{chat}, s.chats...)
}

// syncCurrentChatTitleToChatList syncs the current chat's title back to the
// in-memory sess.chats list. This is necessary because:
//   - addChatToList adds a chat with an empty title (at creation time, no title exists)
//   - OnRestoreSession later derives/sets a title on currentChat but not on sess.chats
//   - OnPutChatTitle updates currentChat.title but previously did not update sess.chats
//
// This causes the sidebar to show stale/empty titles when the frontend re-renders
// from the sess.chats list. Call this after setting a title on currentChat.
// Must be called with session.mu NOT held (locks chatsMu internally).
func (s *session) syncCurrentChatTitleToChatList(title string, titleState int) {
	s.mu.Lock()
	if s.currentChat.dbChat == nil {
		s.mu.Unlock()
		return
	}
	chatID := s.currentChat.dbChat.ID
	s.mu.Unlock()

	if chatID == 0 {
		return
	}

	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	for i := range s.chats {
		if s.chats[i].ID == chatID {
			s.chats[i].Title = title
			s.chats[i].TitleState = int8(titleState)
			return
		}
	}
}

// ============================================================
// DB ->Agent message conversion helpers
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
	if s.currentChat.dbChat == nil {
		return nil, fmt.Errorf("no DB session")
	}
	chatID := s.currentChat.dbChat.ID
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
