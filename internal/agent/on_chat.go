package agent

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
)

// ============================================================
// ChatDelete handler — DELETE /api/chat?sn=XXX
// ============================================================

// OnChatDelete handles DELETE /api/chat — deletes a chat session by SN.
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
	var chatID int
	session.chatsMu.Lock()

	var found bool
	for i := range session.chats {
		if session.chats[i].SN == sn {
			chatID = int(session.chats[i].ID)
			// Remove from in-memory list
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

	// Phase 2: Physically delete from DB
	if err := session.chatStore.PhysicalDelete(chatID, sn); err != nil {
		log.Printf("failed to delete session (sn=%s, id=%d): %v", sn, chatID, err)
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	// Phase 3: If the deleted chat is the current active chat, reset it (under mu lock)
	session.mu.Lock()
	if session.currentChat != nil && session.currentChat.dbChat != nil && session.currentChat.dbChat.ID == int64(chatID) {
		session.currentChat = &chat{}
	}
	session.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// ChatHandler — POST /api/chat handler (core)
// ============================================================

// ChatAgent handles chat requests, integrating RAG retrieval + LLM streaming
//
// ChatAgent uses SessionManager to isolate each user's chat messages by sessionID.
// The frontend only needs to send the user's latest message each time,
// and ChatAgent merges messages with new messages before sending to the actual LLM.
type ChatAgent struct {
	traitSearcher toolimp.TraitSearcher // Personal knowledge base (RAG) search
	webSearcher   toolimp.WebSearcher   // Web search interface

	charLLMClient llm.Client // LLM API client for chat

	sessionManager *SessionManager
	cookieName     string // cookie name for reading/writing sessionID

	// defaultLang is the default language for i18n (e.g., "zh-CN", "en").
	// Used for translating system prompts, tool descriptions, and other
	// content sent to the AI API and frontend.
	defaultLang string
}

// LLMInfo is the response for the LLM info endpoint.
type LLMInfo struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Website string `json:"website"`
}

// OnGetLLMInfo handles GET /api/chat/info/llm requests.
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
// Currently closes the VectorStore (knowledge base) database.
func (h *ChatAgent) Close() error {
	return h.traitSearcher.Close()
}

// NewChatHandler creates a ChatHandler
//
// cookieName: the cookie name for reading/writing sessionID, e.g. "brain_go_session"
// defaultLang: the default language for i18n, e.g. "zh-CN", "en". Empty string defaults to "en".
// anonymousStore: the ChatStore for anonymous users (data/anonymous.db), must not be nil.
func NewChatHandler(
	traitSearcher toolimp.TraitSearcher,
	webSearcher toolimp.WebSearcher,
	chatLLMClient llm.Client,
	cookieName string,
	defaultLang string,
	anonymousStore *store.ChatStore,
) *ChatAgent {
	if defaultLang == "" {
		defaultLang = "en"
	}
	return &ChatAgent{
		traitSearcher:  traitSearcher,
		webSearcher:    webSearcher,
		charLLMClient:  chatLLMClient,
		sessionManager: NewSessionManager(anonymousStore),
		cookieName:     cookieName,
		defaultLang:    defaultLang,
	}
}

func makeAssistantBrokenMessage(lang string, id int64) Message {
	brokenMsg := i18n.TL(lang, "assistant_broken_message")

	return Message{
		ID:        id,
		Role:      llm.RoleAssistant,
		Content:   brokenMsg,
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// ============================================================
// SwitchChat handler — GET /api/chat/switch?sn=XXX
// SwitchChat handler — switches the current active chat to a specified historical chat (topic switch).
// ============================================================

// OnSwitchChat handles GET /api/chat/switch — switches the current
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
		dbMessages, err := session.chatStore.ListMessages(dbSessionID)
		if err == nil {
			msgs = convertDBMessagesToAgentMessages(dbMessages, session.chatStore, dbSessionID)
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
// ChatPin handler — PUT /api/chat/pin?sn=XXX&pinned=true|false
// ChatPin handler — pins/unpins the specified chat.
// ============================================================

// OnChatPin handles PUT /api/chat/pin — toggles the pinned state of a chat.
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

	if err := session.chatStore.UpdateChatPin(targetChat.ID, pinned); err != nil {
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
