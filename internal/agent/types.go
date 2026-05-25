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
	dbChat *store.Chat // Bridge to store.Chat (never nil after creation)

	title      string     // Session title, generated from the first user message content
	titleState TitleState // Title modification state
}

// session represents an individual user's session
type session struct {
	mu      sync.Mutex // protects: currentChat, userNo, lastActivity
	chatsMu sync.Mutex // protects: chats, chatStore

	lastActivity time.Time // Last activity time, used by GC for cleanup

	id          string           // HTTP cookie session ID (e.g., "s-<32hex>-<digits>"), set at creation time
	currentChat *chat            // Current active chat (messages, title, titleState)
	chats       []store.Chat     // User's chat list from the database
	userNo      string           // Global unique user serial number; "anonymous" by default
	chatStore   *store.ChatStore // Chat database store; never nil after Phase A
}

// GetTitle returns the current title and its modification state atomically.
func (s *session) GetTitle() (string, TitleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentChat.title, s.currentChat.titleState
}

// SetTitle sets both the title and its modification state atomically.
// Title is always updated. TitleState only moves forward (0→1, 0→2, 1→2).
func (s *session) SetTitle(newTitle string, newState TitleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentChat.title = newTitle
	if newState > s.currentChat.titleState {
		s.currentChat.titleState = newState
	}
}

// switchToUser switches the session to a logged-in user.
// Since anonymous users now have their own DB (data/anonymous.db),
// there is no need to migrate anonymous messages — they stay in anonymous.db.
// This simply opens the user's DB file and loads their chat list.
func (s *session) switchToUser(sn string) {
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

	// Phase 2: lock and set state
	s.chatsMu.Lock()
	s.chatStore = chatStore
	s.chats = chats
	s.chatsMu.Unlock()

	s.mu.Lock()
	s.currentChat = &chat{}
	s.userNo = sn
	s.mu.Unlock()
}

// switchToChat switches the current active chat to a historical session
// identified by its serial number (SN). It only sets dbChat without loading
// messages into memory. Messages are loaded from DB on demand.
// Returns an error if the session is not found.
func (s *session) switchToChat(sn string) error {
	// Phase 1: Find the chat by SN under chatsMu lock
	s.chatsMu.Lock()

	var foundChat *store.Chat
	for i := range s.chats {
		if s.chats[i].SN == sn {
			foundChat = &s.chats[i]
			break
		}
	}
	s.chatsMu.Unlock()

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

// SessionManager manages all user sessions
type SessionManager struct {
	mu             sync.RWMutex
	sessions       map[string]*session
	anonymousStore *store.ChatStore // ChatStore for anonymous users (data/anonymous.db)
}

// NewSessionManager creates a SessionManager
func NewSessionManager(anonymousStore *store.ChatStore) *SessionManager {
	return &SessionManager{
		sessions:       make(map[string]*session),
		anonymousStore: anonymousStore,
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
		userNo:       "anonymous",
		chatStore:    sm.anonymousStore,
		currentChat:  &chat{},
	}
	sm.sessions[sessionID] = s
	return s
}

// GetMessages loads the session's chat messages from DB and returns them.
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

	// Load messages from DB
	dbSessionID := s.currentChat.dbChat.ID
	if dbSessionID == 0 {
		return []Message{}, s
	}
	dbMessages, err := s.chatStore.ListMessages(dbSessionID)
	if err != nil {
		return []Message{}, s
	}
	msgs := convertDBMessagesToAgentMessages(dbMessages, s.chatStore, dbSessionID)
	return msgs, s
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

	dbSessionID := s.currentChat.dbChat.ID
	if dbSessionID == 0 {
		return fmt.Errorf("no DB session")
	}

	// Delete web sources first, then messages
	if err := s.chatStore.DeleteWebSourcesByGroup(dbSessionID, int(msgID)); err != nil {
		log.Printf("failed to delete web sources for group %d: %v", msgID, err)
	}

	// Delete messages from DB
	return s.chatStore.DeleteMessageGroup(dbSessionID, int(msgID))
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
	dbSessionID := s.currentChat.dbChat.ID
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

// ============================================================
// Phase B 辅助函数 — DB ↔ Agent 消息转换
// ============================================================

// convertDBMessagesToAgentMessages 将 store.Message 切片转换为 agent.Message 切片。
//
// 注意：store.Message 结构体没有 Sources 字段，chat_messages 表也没有
// 存储 web_sources 的列。persistMessageToDB 只持久化了 content 和 reasoning，
// Sources（WebSources）从未写入 DB。
// 因此从 DB 恢复的消息中 Sources 始终为空，页面刷新后前端无法恢复
// WebSources 面板。
//
// v3 设计文档（plans/currentChat-chats-refactor-v3-design.md）已规划了
// web_sources 表和 store.WebSource 结构体，但尚未实现。
// WebSources 持久化是独立的功能增强，不在 Phase B 范围内，
// 将在 Phase B 完成后单独处理。
// convertDBMessagesToAgentMessages 将 store.Message 切片转换为 agent.Message 切片，
// 并从 DB 加载关联的 WebSources 按 group_index 匹配填充。
//
// chatStore 和 sessionID 用于查询 web_sources 表；如果 chatStore 为 nil 或 sessionID 为 0，
// 则 Sources 保持为空（兼容匿名用户等无 DB 场景）。
func convertDBMessagesToAgentMessages(dbMessages []store.Message, chatStore *store.ChatStore, sessionID int64) []Message {
	// Load web sources for this session (if available)
	var sourcesByMsgID map[int64][]store.WebSource
	if chatStore != nil && sessionID > 0 {
		var err error
		sourcesByMsgID, err = chatStore.ListWebSourcesBySession(sessionID)
		if err != nil {
			log.Printf("failed to list web sources for session %d: %v", sessionID, err)
		}
	}

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
	return msgs
}

// loadMessagesAsLLMMessages 从 DB 加载消息并转换为 llm.Message 切片。
// 调用者必须持有 session.mu。
func loadMessagesAsLLMMessages(s *session) ([]llm.Message, error) {
	dbSessionID := s.currentChat.dbChat.ID
	if dbSessionID == 0 {
		return nil, fmt.Errorf("no DB session")
	}
	dbMessages, err := s.chatStore.ListMessages(dbSessionID)
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
