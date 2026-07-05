package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"BrainForever/infra/embedder"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/infra/zylog"
	"BrainForever/internal/local/agent/toolimp"
	"BrainForever/internal/local/store"
)

// ============================================================
// ChatDelete handler -DELETE /api/chat?sn=XXX
// ============================================================

// OnChatDelete handles DELETE /api/chat -soft-deletes (moves to trash) a chat session by SN.
// Also removes it from the in-memory chat list. If the deleted chat is the
// current active chat, resets the current chat to nil.
func (h *ChatAgent) OnChatDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Phase 1: Find the chat by SN and remove from in-memory list (under chatsMu lock)
	var chatID int64
	session.chatsMu.Lock()

	var found bool
	for i := range session.chats {
		if session.chats[i].SN == sn {
			chatID = session.chats[i].ID
			// Remove from in-memory list (normal chats)
			session.chats = append(session.chats[:i], session.chats[i+1:]...)
			found = true
			break
		}
	}
	session.chatsMu.Unlock()

	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if chatID <= 0 {
		http.Error(w, "invalid chat ID", http.StatusInternalServerError)
		return
	}

	// Phase 2: If the deleted chat is the current active chat, reset it (under mu lock)
	//  Must execute before LogicDelete (I/O operation) to avoid race conditions:
	//   If OnNewMessage acquires mu between chatsMu unlock and mu lock,
	//   it will find currentChat.dbChat still pointing to the deleted chat (non-nil),
	//   causing ensureSessionDBForChat to return early, writing new messages to the deleted chat.
	//   Moving reset before LogicDelete + immediately after chatsMu unlock
	//   reduces the race window from millisecond-level I/O duration to a few nanoseconds of CPU instruction gap.
	session.mu.Lock()
	if session.currentChat != nil && session.currentChat.dbChat != nil && session.currentChat.dbChat.ID == chatID {
		session.currentChat = &chat{}
	}
	session.mu.Unlock()

	// Phase 3: Soft-delete (logic delete) -move to trash
	if err := session.chatsStore.LogicDelete(sn); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// ListDeletedChats handler -GET /api/chat/deleted
// ============================================================

// OnListDeletedChats handles GET /api/chat/deleted -returns the list of
// soft-deleted (trashed) chats for the current session's user.
func (h *ChatAgent) OnListDeletedChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.chatsMu.Lock()
	chatStore := session.chatsStore
	session.chatsMu.Unlock()

	deletedChats, err := chatStore.ListDeletedChats(100)
	if err != nil {
		http.Error(w, "failed to list deleted chats", http.StatusInternalServerError)
		return
	}

	if deletedChats == nil {
		deletedChats = []store.Chat{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chats": deletedChats,
	})
}

// ============================================================
// RestoreChat handler -PUT /api/chat/restore?sn=XXX
// ============================================================

// OnRestoreChat handles PUT /api/chat/restore -restores a soft-deleted chat
// and adds it back to the in-memory chat list.
func (h *ChatAgent) OnRestoreChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Restore in DB
	session.chatsMu.Lock()
	chatStore := session.chatsStore
	session.chatsMu.Unlock()

	if err := chatStore.RestoreChat(sn); err != nil {
		http.Error(w, "failed to restore chat", http.StatusInternalServerError)
		return
	}

	// Reload the restored chat from DB and add back to in-memory list
	session.chatsMu.Lock()
	chats, err := session.chatsStore.ListChats(100)
	if err == nil {
		session.chats = chats
	}
	session.chatsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// PermanentDelete handler -DELETE /api/chat/permanent?sn=XXX
// ============================================================

// OnPermanentDelete handles DELETE /api/chat/permanent -permanently deletes
// a soft-deleted chat (physical delete from DB).
func (h *ChatAgent) OnPermanentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Find the chat from DB (regardless of deleted status)
	session.chatsMu.Lock()
	chatStore := session.chatsStore
	session.chatsMu.Unlock()

	chat, err := chatStore.FindChatBySN(sn)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	chatID := chat.ID

	if err := chatStore.PhysicalDelete(int(chatID), sn); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// EmptyTrash handler -DELETE /api/chat/trash
// ============================================================

// OnEmptyTrash handles DELETE /api/chat/empty-trash -permanently deletes
// all soft-deleted chats (clears the recycle bin).
func (h *ChatAgent) OnEmptyTrash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.chatsMu.Lock()
	chatStore := session.chatsStore
	session.chatsMu.Unlock()

	if err := chatStore.EmptyTrash(); err != nil {
		http.Error(w, "failed to empty trash", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// ChatHandler -POST /api/chat handler (core)
// ============================================================

// ChatAgent handles chat requests, integrating RAG retrieval + LLM streaming
//
// ChatAgent uses SessionManager to isolate each user's chat messages by sessionID.
// The frontend only needs to send the user's latest message each time,
// and ChatAgent merges messages with new messages before sending to the actual LLM.
type ChatAgent struct {
	embedder    embedder.Embedder   // Text embedder for trait extraction and future RAG
	webSearcher toolimp.WebSearcher // Web search interface

	charLLMClient llm.Client // LLM API client for chat

	sessionManager *SessionManager
	cookieName     string // cookie name for reading/writing sessionID

	// defaultLang is the default language for i18n (e.g., "zh-CN", "en").
	// Used for translating system prompts, tool descriptions, and other
	// content sent to the AI API and frontend.
	defaultLang string

	// avatarDir is the filesystem path to the avatar image directory.
	// Used by OnLogin to dynamically discover available avatar files.
	avatarDir string

	logger zylog.Logger // Structured logger for the agent
}

// LLMInfo is the response for the LLM info endpoint.
type LLMInfo struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Website string `json:"website"`
}

// OnGetLLMInfo handles GET /api/info/llm/chat requests.
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

// Close releases all underlying resources held by the ChatAgent.
// Closes all database stores (ChatStore, VectorStore) in all sessions,
// including the shared anonymous store.
func (h *ChatAgent) Close() error {
	h.sessionManager.Close()
	return nil
}

// NewChatHandler creates a ChatHandler
//
// cookieName: the cookie name for reading/writing sessionID, e.g. "brain_go_session"
// defaultLang: the default language for i18n, e.g. "zh-CN", "en". Empty string defaults to "en".
// anonymousStore: the ChatStore for anonymous users (data/anonymous.db), must not be nil.
func NewChatHandler(
	embedder embedder.Embedder,
	webSearcher toolimp.WebSearcher,
	chatLLMClient llm.Client,
	cookieName string,
	defaultLang string,
	anonymousStore *store.ChatStore,
	avatarDir string,
	logger zylog.Logger,
) *ChatAgent {
	if defaultLang == "" {
		defaultLang = "en"
	}
	return &ChatAgent{
		embedder:       embedder,
		webSearcher:    webSearcher,
		charLLMClient:  chatLLMClient,
		sessionManager: NewSessionManager(anonymousStore, embedder.Dimension(), logger),
		cookieName:     cookieName,
		defaultLang:    defaultLang,
		avatarDir:      avatarDir,
		logger:         logger,
	}
}

func makeAssistantBrokenMessage(lang string, id int64) Message {
	brokenMsg := i18n.TL(lang, "assistant_broken_message")

	return Message{
		ID:        id,
		Role:      llm.RoleAssistant,
		Content:   brokenMsg,
		CreatedAt: time.Now().UTC(),
	}
}

// ============================================================
// SwitchChat handler -GET /api/chat/switch?sn=XXX
// SwitchChat handler -switches the current active chat to a specified historical chat (topic switch).
// ============================================================

// OnSwitchChat handles GET /api/chat/switch -switches the current
// active chat to a historical chat identified by its SN, loading
// its messages from the database. Returns the chat's
// messages, title, and title state.
func (h *ChatAgent) OnSwitchChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	if err := session.switchToChat(sn); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Determine language for i18n (used by ensureAssistantForOrphanUser)
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// Load messages from DB (no in-memory storage)
	session.mu.Lock()
	chatSN := session.currentChat.dbChat.SN
	session.mu.Unlock()

	var msgs []Message
	if chatSN != "" {
		dbMessages, err := session.chatsStore.ListMessages(chatSN)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to list messages: %v", err), http.StatusInternalServerError)
			return
		}
		agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, session.chatsStore, chatSN)
		if convErr != nil {
			http.Error(w, fmt.Sprintf("failed to load web sources: %v", convErr), http.StatusInternalServerError)
			return
		}
		msgs = agentMsgs
	}
	if msgs == nil {
		msgs = []Message{}
	}

	// Compensate orphan user message: if the last message is a user message
	// (without a corresponding assistant reply), append a broken assistant message
	// to ensure the frontend displays the correct interruption prompt.
	msgs = ensureAssistantForOrphanUser(msgs, lang)

	session.mu.Lock()
	title := session.currentChat.title
	titleState := int(session.currentChat.titleState)
	session.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"messages":    msgs,
		"title":       title,
		"title_state": int(titleState),
	})
}

// ============================================================
// ChatPin handler -PUT /api/chat/pin?sn=XXX&pinned=true|false
// ChatPin handler -pins/unpins the specified chat.
// ============================================================

// OnChatPin handles PUT /api/chat/pin -toggles the pinned state of a chat.
// Uses chatsMu because it operates on session.chats (independent of streaming).
func (h *ChatAgent) OnChatPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, "sn query parameter is required", http.StatusBadRequest)
		return
	}

	pinnedStr := r.URL.Query().Get("pinned")
	pinned := pinnedStr == "true"

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.chatsMu.Lock()
	defer session.chatsMu.Unlock()

	// Find the session by SN
	var targetChat *store.Chat
	for i := range session.chats {
		if session.chats[i].SN == sn {
			targetChat = &session.chats[i]
			break
		}
	}
	if targetChat == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if err := session.chatsStore.UpdateChatPin(targetChat.ID, pinned); err != nil {
		http.Error(w, "failed to update chat pin", http.StatusInternalServerError)
		return
	}

	// Update in-memory cache
	targetChat.Pinned = pinned

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
