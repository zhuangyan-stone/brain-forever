package agent

import (
	"encoding/json"
	"log"
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
	//  必须在 LogicDelete（I/O 操作）之前执行，以避免竞态条件：
	//   如果 OnNewMessage 在 chatsMu unlock 和 mu lock 之间获取到 mu，
	//   会发现 currentChat.dbChat 仍然指向被删除的 chat（非 nil），
	//   导致 ensureSessionDBForChat 直接 return，将新消息写入已删除的 chat。
	//   将 reset 移到 LogicDelete 之前 + 紧跟在 chatsMu unlock 之后，
	//   可将竞争窗口从毫秒级 I/O 时长"缩小到几纳秒的 CPU 指令间隙"。
	session.mu.Lock()
	if session.currentChat != nil && session.currentChat.dbChat != nil && session.currentChat.dbChat.ID == chatID {
		session.currentChat = &chat{}
	}
	session.mu.Unlock()

	// Phase 3: Soft-delete (logic delete) -move to trash
	if err := session.chatsStore.LogicDelete(sn); err != nil {
		log.Printf("failed to logic-delete session (sn=%s): %v", sn, err)
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
		log.Printf("failed to list deleted chats: %v", err)
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
		log.Printf("failed to restore chat (sn=%s): %v", sn, err)
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
		log.Printf("failed to permanently delete session (sn=%s): %v", sn, err)
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
		log.Printf("failed to empty trash: %v", err)
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

// Close releases underlying resources held by the ChatHandler.
func (h *ChatAgent) Close() error {
	return nil // No global VectorStore to close; per-user traits stores are closed per session.
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
	logger zylog.Logger,
) *ChatAgent {
	if defaultLang == "" {
		defaultLang = "en"
	}
	return &ChatAgent{
		embedder:       embedder,
		webSearcher:    webSearcher,
		charLLMClient:  chatLLMClient,
		sessionManager: NewSessionManager(anonymousStore),
		cookieName:     cookieName,
		defaultLang:    defaultLang,
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
	dbSessionID := session.currentChat.dbChat.ID
	session.mu.Unlock()

	var msgs []Message
	if dbSessionID > 0 {
		dbMessages, err := session.chatsStore.ListMessages(dbSessionID)
		if err == nil {
			msgs = convertDBMessagesToAgentMessages(dbMessages, session.chatsStore, dbSessionID)
		}
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
		log.Printf("failed to update chat pin: %v", err)
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
