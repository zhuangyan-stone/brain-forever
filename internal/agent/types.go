package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"BrainForever/internal/agent/toolimp"
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

// session represents an individual user's session
type session struct {
	mu           sync.Mutex
	history      []Message // The user's complete chat history
	lastActivity time.Time // Last activity time, used by GC for cleanup
	title        string    // Session title, generated from the first user message content
}

func (s *session) GetTitle() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.title
}

func (s *session) SetTitle(newTitle string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if newTitle != s.title {
		s.title = newTitle
	}
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
		lastActivity: time.Now(),
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
	cp := make([]Message, len(s.history))
	copy(cp, s.history)
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

	// Find the first message with the given ID
	start := -1
	for i, msg := range s.history {
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
	for end < len(s.history) && s.history[end].ID == msgID {
		end++
	}

	s.history = append(s.history[:start], s.history[end:]...)
	return nil
}
