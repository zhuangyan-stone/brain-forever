package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

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
// Session management — isolates user chat history by sessionID
// ============================================================

// TitleState represents the state of the session title modification.
//
//	0: original title (default, "新对话" for new sessions)
//	1: AI-modified title
//	2: user-modified title
type TitleState int

const (
	TitleStateOriginal     TitleState = iota // 0: original title
	TitleStateAIModified                     // 1: AI-modified title
	TitleStateUserModified                   // 2: user-modified title
)

type chat struct {
	history []Message // The user's complete chat history

	title      string     // Session title, generated from the first user message content
	titleState TitleState // Title modification state

	dbSessionID int64 // Corresponding chat_sessions.id in the database (0 means not persisted)
}

// session represents an individual user's session
type session struct {
	mu           sync.Mutex
	lastActivity time.Time // Last activity time, used by GC for cleanup

	currentChat *chat // Current active chat (history, title, titleState)

	chats     []store.Session  // User's session list from the database (populated after login)
	userNo    string           // Global unique user serial number; empty means not logged in
	chatStore *store.ChatStore // Chat database store for the logged-in user; nil for anonymous users
}

func (s *session) GetTitle() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentChat == nil {
		return ""
	}
	return s.currentChat.title
}

func (s *session) SetTitle(newTitle string) {
	s.mu.Lock() // <---- 死锁
	defer s.mu.Unlock()

	if s.currentChat == nil {
		s.currentChat = &chat{}
	}
	if newTitle != s.currentChat.title {
		s.currentChat.title = newTitle
	}
}

func (s *session) GetTitleState() TitleState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentChat == nil {
		return TitleStateOriginal
	}
	return s.currentChat.titleState
}

// SetTitleState sets the title modification state.
// The state can only move forward (0→1, 0→2, 1→2), never backward.
// Returns true if the state was updated, false if the new state is lower than the current state.
func (s *session) SetTitleState(newState TitleState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentChat == nil {
		s.currentChat = &chat{}
	}
	if newState > s.currentChat.titleState {
		s.currentChat.titleState = newState
		return true
	}
	return false
}

// switchToUser switches the session to a logged-in user.
// It clears the current chat, opens (or creates)
// the user's chat database, and loads the user's session list.
func (s *session) switchToUser(sn string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset current chat (history, title, titleState)
	s.currentChat = nil
	// Set the user serial number
	s.userNo = sn

	// Open (or create) the user's chat database
	dbFile := "data/" + sn + ".chats.db"
	chatStore, err := store.CreateLocalChatScheme(dbFile)
	if err != nil {
		log.Printf("failed to create local chat scheme for user %s: %v", sn, err)
		return
	}

	// Save the chat store for later use (message persistence)
	s.chatStore = chatStore

	// Load the user's session list (latest 100)
	chats, err := chatStore.ListSessions(100)
	if err != nil {
		log.Printf("failed to list sessions for user %s: %v", sn, err)
		return
	}
	s.chats = chats
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
		log.Printf("[DEBUG] GetOrCreate: session %s exists, acquiring s.mu", sessionID)
		s.mu.Lock()
		log.Printf("[DEBUG] GetOrCreate: session %s acquired s.mu", sessionID)
		s.lastActivity = time.Now()
		s.mu.Unlock()
		log.Printf("[DEBUG] GetOrCreate: session %s released s.mu", sessionID)
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
		lastActivity: time.Now(),
		currentChat:  &chat{},
	}
	sm.sessions[sessionID] = s
	return s
}

// GetHistory returns a read-only copy of the session's chat history.
// Returns nil if the session does not exist.
func (sm *SessionManager) GetHistory(sessionID string) ([]Message, *session) {
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
	cp := make([]Message, len(s.currentChat.history))
	copy(cp, s.currentChat.history)
	return cp, s
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
	start := -1
	for i, msg := range s.currentChat.history {
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
	for end < len(s.currentChat.history) && s.currentChat.history[end].ID == msgID {
		end++
	}

	s.currentChat.history = append(s.currentChat.history[:start], s.currentChat.history[end:]...)
	return nil
}
