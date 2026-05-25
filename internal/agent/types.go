package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
	"BrainForever/toolset"
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

	CreatedAt string `json:"created_at"` // UTC time string, e.g. "2026-05-02T16:30:00Z"
}

// ChatRequest is the chat request sent from the frontend
type ChatRequest struct {
	Message          Message `json:"message"`
	Stream           bool    `json:"stream"` // Always true
	DeepThink        bool    `json:"deep_think"`
	WebSearchEnabled bool    `json:"web_search_enabled"`
}

// ============================================================
// SSE event type (business-specific, used by ChatHandler)
// ============================================================

// SSEEvent is the SSE event sent to the frontend
type SSEEvent struct {
	Type       string                `json:"type"`              // reasoning | reasoning_end | text | sources | title | done | error
	Subject    string                `json:"subject,omitempty"` // reasoning -> "", "pend"
	Tool       string                `json:"tool,omitempty"`
	Content    string                `json:"content,omitempty"`     // Used for text type, title type
	Sources    []toolimp.TraitSource `json:"sources,omitempty"`     // Used for sources type (RAG sources)
	WebSources []toolimp.WebSource   `json:"web_sources,omitempty"` // Used for sources type (web search sources)
	Usage      *Usage                `json:"usage,omitempty"`       // Used for done type
	Message    string                `json:"message,omitempty"`     // Used for error type
	MsgID      int64                 `json:"msg_id,omitempty"`      // Used for done type — ID of the user message
	CreatedAt  string                `json:"created_at,omitempty"`  // Used for done type — assistant message creation time
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	IsEstimated      bool `json:"is_estimated"` // true if any of the token counts was estimated client-side (not from the LLM API)
}

// ============================================================
// Session management — isolates user chat messages by sessionID
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
	messages []Message // The user's complete chat messages

	title      string     // Session title, generated from the first user message content
	titleState TitleState // Title modification state

	dbSessionID int64 // Corresponding chat_sessions.id in the database (0 means not persisted)
}

// session represents an individual user's session
type session struct {
	mu      sync.Mutex // protects: currentChat, userNo, lastActivity
	chatsMu sync.Mutex // protects: chats, chatStore

	lastActivity time.Time // Last activity time, used by GC for cleanup

	id          string           // HTTP cookie session ID (e.g., "s-<32hex>-<digits>"), set at creation time
	currentChat *chat            // Current active chat (messages, title, titleState)
	chats       []store.Chat     // User's chat list from the database (populated after login)
	userNo      string           // Global unique user serial number; empty means not logged in
	chatStore   *store.ChatStore // Chat database store for the logged-in user; nil for anonymous users
}

// GetTitle returns the current title and its modification state atomically.
func (s *session) GetTitle() (string, TitleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getTitleWithoutLock()
}

func (s *session) getTitleWithoutLock() (string, TitleState) {
	if s.currentChat == nil {
		return "", TitleStateOriginal
	}
	return s.currentChat.title, s.currentChat.titleState
}

// setTitleWithoutLock sets both title and titleState atomically (caller must hold s.mu).
// Title is always updated. TitleState only moves forward (0→1, 0→2, 1→2).
func (s *session) setTitleWithoutLock(newTitle string, newState TitleState) {
	if s.currentChat == nil {
		s.currentChat = &chat{}
	}
	s.currentChat.title = newTitle
	if newState > s.currentChat.titleState {
		s.currentChat.titleState = newState
	}
}

// SetTitle sets both the title and its modification state atomically.
// Title is always updated. TitleState only moves forward (0→1, 0→2, 1→2).
func (s *session) SetTitle(newTitle string, newState TitleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setTitleWithoutLock(newTitle, newState)
}

// ============================================================
// Messages accessors (WithoutLock variants — caller must hold s.mu)
// ============================================================

func (s *session) getMessagesLenWithoutLock() int {
	if s.currentChat == nil {
		return 0
	}
	return len(s.currentChat.messages)
}

func (s *session) getMessagesLastMsgWithoutLock() *Message {
	if s.currentChat == nil || len(s.currentChat.messages) == 0 {
		return nil
	}
	return &s.currentChat.messages[len(s.currentChat.messages)-1]
}

func (s *session) appendMessagesWithoutLock(msgs ...Message) {
	if s.currentChat == nil {
		s.currentChat = &chat{}
	}
	s.currentChat.messages = append(s.currentChat.messages, msgs...)
}

func (s *session) deleteMessagesRangeWithoutLock(start, end int) {
	if s.currentChat == nil {
		return
	}
	s.currentChat.messages = append(s.currentChat.messages[:start], s.currentChat.messages[end:]...)
}

func (s *session) copyMessagesWithoutLock() []Message {
	if s.currentChat == nil {
		return nil
	}
	cp := make([]Message, len(s.currentChat.messages))
	copy(cp, s.currentChat.messages)
	return cp
}

// getMessagesWithoutLock returns the raw messages slice for read-only access
// (caller must hold s.mu).
func (s *session) getMessagesWithoutLock() []Message {
	if s.currentChat == nil {
		return nil
	}
	return s.currentChat.messages
}

// ============================================================
// dbSessionID accessors (WithoutLock variants — caller must hold s.mu)
// ============================================================

func (s *session) getDbSessionIDWithoutLock() int64 {
	if s.currentChat == nil {
		return 0
	}
	return s.currentChat.dbSessionID
}

func (s *session) setDbSessionIDWithoutLock(id int64) {
	if s.currentChat == nil {
		s.currentChat = &chat{}
	}
	s.currentChat.dbSessionID = id
}

// switchToUser switches the session to a logged-in user.
// It preserves the anonymous chat messages by persisting them
// to the user's database, then loads the user's session list.
func (s *session) switchToUser(sn string) {
	// Phase 0: Capture anonymous chat state (under mu lock)
	var anonymousMessages []Message
	var anonymousTitle string
	var anonymousTitleState TitleState
	hasAnonymousMessages := false

	s.mu.Lock()
	if s.currentChat != nil && len(s.currentChat.messages) > 0 {
		anonymousMessages = s.copyMessagesWithoutLock()
		anonymousTitle, anonymousTitleState = s.getTitleWithoutLock()
		hasAnonymousMessages = true
	}
	s.mu.Unlock()

	// Phase 1: IO operations (no lock needed — DB creation + query)
	dbFile := "data/" + sn + ".chats.db"
	chatStore, err := store.CreateLocalChatScheme(dbFile)
	if err != nil {
		log.Printf("failed to create local chat scheme for user %s: %v", sn, err)
		return
	}

	// Load the user's chat list (latest 100)
	chats, err := chatStore.ListChats(100)
	if err != nil {
		log.Printf("failed to list sessions for user %s: %v", sn, err)
		return
	}

	// Phase 1.5: Persist anonymous messages to the user's database
	var mergedDBSessionID int64
	if hasAnonymousMessages {
		chatSN := generateSessionSN()

		// Determine title: use existing title, or derive from first user message
		title := anonymousTitle
		if title == "" {
			for _, msg := range anonymousMessages {
				if msg.Role == llm.RoleUser {
					title = toolset.TruncateTitle(msg.Content, 50)
					break
				}
			}
		}

		dbChat, err := chatStore.InsertChat(chatSN, 0, title, int8(anonymousTitleState))
		if err != nil {
			log.Printf("failed to create DB chat for migrated anonymous chat: %v", err)
		} else {
			mergedDBSessionID = dbChat.ID

			// Persist each message from the anonymous messages
			for _, msg := range anonymousMessages {
				// Map agent.Message role to store.Message role: 0=user, 1=assistant
				var role int
				switch msg.Role {
				case llm.RoleUser:
					role = 0
				case llm.RoleAssistant:
					role = 1
				default:
					continue // Skip system messages
				}

				groupIndex := int(msg.ID)

				var reasoning *string
				if msg.Reasoning != "" {
					reasoning = &msg.Reasoning
				}

				if err := chatStore.InsertMessage(
					mergedDBSessionID,
					groupIndex,
					role,
					msg.Content,
					reasoning,
				); err != nil {
					log.Printf("failed to persist anonymous message to user DB: %v", err)
				}
			}

			// Add the merged chat to the top of the session list
			newChat := store.Chat{
				ID:         dbChat.ID,
				SN:         chatSN,
				Title:      title,
				TitleState: int8(anonymousTitleState),
				CreateAt:   dbChat.CreateAt,
				UpdateAt:   dbChat.UpdateAt,
			}
			chats = append([]store.Chat{newChat}, chats...)
		}
	}

	// Phase 2: lock and set state
	s.chatsMu.Lock()
	s.chatStore = chatStore
	s.chats = chats
	s.chatsMu.Unlock()

	s.mu.Lock()
	if hasAnonymousMessages && mergedDBSessionID > 0 {
		// Preserve the merged anonymous chat as the current active session.
		// The frontend's GET /api/session call will return these messages,
		// allowing a seamless transition from anonymous to logged-in user
		// without losing the conversation context.
		s.currentChat = &chat{
			messages:    anonymousMessages,
			title:       anonymousTitle,
			titleState:  anonymousTitleState,
			dbSessionID: mergedDBSessionID,
		}
	} else {
		s.currentChat = nil
	}
	s.userNo = sn
	s.mu.Unlock()
}

// switchToChat switches the current active chat to a historical session
// identified by its serial number (SN). It loads the session's messages
// from the database into memory and sets them as the current chat.
// Returns an error if the user is not logged in or the session is not found.
func (s *session) switchToChat(sn string) error {
	// Phase 1: Find the chat by SN under chatsMu lock
	s.chatsMu.Lock()
	if s.chatStore == nil {
		s.chatsMu.Unlock()
		return fmt.Errorf("user not logged in")
	}

	var dbSessionID int64
	var targetTitle string
	var targetTitleState int8
	found := false
	for i := range s.chats {
		if s.chats[i].SN == sn {
			dbSessionID = s.chats[i].ID
			targetTitle = s.chats[i].Title
			targetTitleState = s.chats[i].TitleState
			found = true
			break
		}
	}
	s.chatsMu.Unlock()

	if !found {
		return fmt.Errorf("session not found: %s", sn)
	}

	// Phase 2: Load messages from DB (no lock needed — IO)
	dbMessages, err := s.chatStore.ListMessages(dbSessionID)
	if err != nil {
		return fmt.Errorf("failed to load messages for session %s: %w", sn, err)
	}

	// Phase 3: Convert store.Message slice to agent.Message slice
	msgs := make([]Message, 0, len(dbMessages))
	for _, m := range dbMessages {
		role := llm.RoleUser
		if m.Role == 1 {
			role = llm.RoleAssistant
		}

		agentMsg := Message{
			ID:        int64(m.GroupIndex),
			Role:      role,
			Content:   m.Content,
			CreatedAt: m.CreateAt,
		}
		if m.Reasoning != nil {
			agentMsg.Reasoning = *m.Reasoning
		}
		// NOTE: Usage and Sources are not persisted to DB yet,
		// so they will be empty after switching sessions.
		// This is acceptable for the current implementation.
		msgs = append(msgs, agentMsg)
	}

	// Phase 4: Set as current chat under mu lock
	s.mu.Lock()
	s.currentChat = &chat{
		messages:    msgs,
		title:       targetTitle,
		titleState:  TitleState(targetTitleState),
		dbSessionID: dbSessionID,
	}
	s.mu.Unlock()

	return nil
}

// SessionManager manages all user sessions
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*session
}

// NewSessionManager creates a SessionManager
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*session),
	}
}

// GetOrCreate gets or creates a session for the given sessionID
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
		currentChat:  &chat{},
	}
	sm.sessions[sessionID] = s
	return s
}

// GetMessages returns a read-only copy of the session's chat messages.
// Returns nil if the session does not exist.
func (sm *SessionManager) GetMessages(sessionID string) ([]Message, *session) {
	sm.mu.RLock()
	s, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = time.Now()
	if s.currentChat == nil {
		return []Message{}, s
	}
	return s.copyMessagesWithoutLock(), s
}

// Remove removes the session for the given sessionID (optional)
func (sm *SessionManager) Remove(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, sessionID)
}

// sessionTTL is the time-to-live for idle sessions.
// Sessions idle longer than this will be cleaned up by GC.
const sessionTTL = 7 * 24 * time.Hour // 7 days, matching cookie MaxAge

// gcInterval is how often the GC goroutine runs.
const gcInterval = 1 * time.Hour

// gcMinSessions is the minimum number of sessions before GC bothers to check timestamps.
// When the total session count is below this threshold, GC is a no-op.
const gcMinSessions = 5

// GC cleans up sessions that have been idle longer than sessionTTL.
// It is safe for concurrent use.
// As an optimization, if the total session count is below gcMinSessions,
// GC returns immediately without checking any timestamps.
func (sm *SessionManager) GC() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Optimization: skip timestamp checks when there are few sessions
	if len(sm.sessions) < gcMinSessions {
		return
	}

	now := time.Now()
	for id, s := range sm.sessions {
		s.mu.Lock()
		idle := now.Sub(s.lastActivity)
		s.mu.Unlock()

		if idle > sessionTTL {
			delete(sm.sessions, id)
		}
	}
}

// StartGC starts a background goroutine that periodically runs GC.
// The goroutine stops when the given context is cancelled.
func (sm *SessionManager) StartGC(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(gcInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sm.GC()
			}
		}
	}()
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

	if s.currentChat == nil {
		return fmt.Errorf("no active chat")
	}

	// Find the first message with the given ID
	msgs := s.getMessagesWithoutLock()
	start := -1
	for i, msg := range msgs {
		if msg.ID == msgID {
			start = i
			break
		}
	}

	if start < 0 {
		return fmt.Errorf("message with ID %d not found", msgID)
	}

	// Find the end: keep deleting while ID matches, stop at first different ID
	end := start + 1
	for end < len(msgs) && msgs[end].ID == msgID {
		end++
	}

	s.deleteMessagesRangeWithoutLock(start, end)
	return nil
}

// addChatToList adds a store.Chat to the in-memory chat list (session.chats)
// if it's not already present. Must be called with session.mu NOT held
// (it locks chatsMu internally).
// This is called from ensureDBSession after creating a new DB chat record,
// so that the new chat immediately appears in the left sidebar's chat list.
func (s *session) addChatToList(chat store.Chat) {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()

	if s.chatStore == nil {
		return
	}

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
	if s.chatStore == nil {
		return
	}

	s.mu.Lock()
	dbSessionID := s.getDbSessionIDWithoutLock()
	s.mu.Unlock()

	if dbSessionID == 0 {
		return
	}

	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	for i := range s.chats {
		if s.chats[i].ID == dbSessionID {
			s.chats[i].Title = title
			s.chats[i].TitleState = int8(titleState)
			return
		}
	}
}
