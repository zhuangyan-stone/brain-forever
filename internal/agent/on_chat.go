package agent

import (
	"encoding/json"
	"net/http"
	"time"

	"BrainForever/infra/embedder"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/infra/zylog"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
	"BrainForever/internal/store/dbc"
)

// ============================================================
// ChatDelete handler -DELETE /api/chat?sn=XXX
// ============================================================

// OnChatDelete handles DELETE /api/chat -soft-deletes (moves to trash) a chat session by SN.
// Also removes it from the in-memory chat list. If the deleted chat is the
// current active chat, resets the current chat to nil.
func (h *ChatAgent) OnChatDelete(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Phase 1: Find the chat by SN and remove from in-memory list (under chatsMu lock)
	var chatID int64
	session.user.chatsMu.Lock()

	var found bool
	for i := range session.user.chats {
		if session.user.chats[i].SN == sn {
			chatID = session.user.chats[i].ID
			// Remove from in-memory list (normal chats)
			session.user.chats = append(session.user.chats[:i], session.user.chats[i+1:]...)
			found = true
			break
		}
	}
	session.user.chatsMu.Unlock()

	if !found {
		http.Error(w, i18n.T("db_session_not_found"), http.StatusNotFound)
		return
	}

	if chatID <= 0 {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "invalid chat ID"}), http.StatusInternalServerError)
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
	if session.user.currentChat != nil && session.user.currentChat.dbChat != nil && session.user.currentChat.dbChat.ID == chatID {
		session.user.currentChat = &chat{}
	}
	session.mu.Unlock()

	// Phase 3: Soft-delete (logic delete) -move to trash
	chatStore, cerr := h.openChatDB(session)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	if err := chatStore.LogicDelete(sn); err != nil {
		h.closeChatDB(chatStore)
		http.Error(w, i18n.T("api_error_failed_to_delete_session"), http.StatusInternalServerError)
		return
	}
	h.closeChatDB(chatStore)

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
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	chatStore, err := h.openChatDB(session)
	if err != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	deletedChats, err := chatStore.ListDeletedChats(100)
	if err != nil {
		http.Error(w, i18n.T("db_list_deleted_chats_failed"), http.StatusInternalServerError)
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
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	chatStore, cerr := h.openChatDB(session)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	// Restore in DB
	if err := chatStore.RestoreChat(sn); err != nil {
		http.Error(w, i18n.T("db_restore_chat_failed"), http.StatusInternalServerError)
		return
	}

	// Reload the restored chat from DB and add back to in-memory list
	chats, err := chatStore.ListChats(100)
	if err == nil {
		session.user.chatsMu.Lock()
		session.user.chats = chats
		session.user.chatsMu.Unlock()
	}

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
// Also removes all associated traits, vectors, and keywords from the brain DB.
func (h *ChatAgent) OnPermanentDelete(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	chatStore, cerr := h.openChatDB(session)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	chat, err := chatStore.FindChatBySN(sn)
	if err != nil {
		http.Error(w, i18n.T("db_session_not_found"), http.StatusNotFound)
		return
	}

	chatID := chat.ID

	// Phase 1: Delete traits from brain DB (cross-table) BEFORE deleting the chat.
	// This removes all personal traits associated with this chat SN,
	// along with their vector embeddings (trait_vectors) and keywords (via FK cascade).
	traitsStore, terr := h.openBrainDB(session)
	if terr != nil {
		h.logger.Errorf("failed to open traits store for chat %s: %v", sn, terr)
	} else {
		if _, err := traitsStore.DeleteByChatSN(sn); err != nil {
			h.logger.Errorf("failed to delete traits for chat %s: %v", sn, err)
			// Non-fatal: continue with chat deletion even if trait deletion fails
		}
		h.closeBrainDB(traitsStore)
	}

	// Phase 2: Delete the chat session from chats DB.
	// Child rows (chat_messages, web_sources, chat_tags, chat_favorites)
	// are automatically removed via ON DELETE CASCADE.
	if err := chatStore.PhysicalDelete(int(chatID), sn); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_delete_session"), http.StatusInternalServerError)
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
// Also removes all associated traits, vectors, and keywords from the brain DB
// for every trashed chat.
func (h *ChatAgent) OnEmptyTrash(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	chatStore, cerr := h.openChatDB(session)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	// Phase 1: Collect all trashed chat SNs.
	deletedChats, err := chatStore.ListDeletedChats(1000)
	if err != nil {
		http.Error(w, i18n.T("db_list_deleted_chats_failed"), http.StatusInternalServerError)
		return
	}

	// Phase 2: Delete traits from brain DB for each trashed chat (cross-table).
	traitsStore, terr := h.openBrainDB(session)
	if terr != nil {
		h.logger.Errorf("failed to open traits store: %v", terr)
	} else {
		sns := make([]string, 0, len(deletedChats))
		for _, c := range deletedChats {
			if c.SN != "" {
				sns = append(sns, c.SN)
			}
		}
		if len(sns) > 0 {
			if _, err := traitsStore.DeleteTraitsByChatSNs(sns); err != nil {
				h.logger.Errorf("failed to delete traits for trashed chats: %v", err)
				// Non-fatal: continue with trash emptying even if trait deletion fails
			}
		}
		h.closeBrainDB(traitsStore)
	}

	// Phase 3: Delete all trashed chat sessions from chats DB.
	// Child rows (chat_messages, web_sources, chat_tags, chat_favorites)
	// are automatically removed via ON DELETE CASCADE.
	if err := chatStore.EmptyTrash(); err != nil {
		http.Error(w, i18n.T("db_delete_trashed_sessions_failed"), http.StatusInternalServerError)
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
//
// Note: API clients (LLM, Embedder, Web Search) are not stored here.
// They are managed as package-level singletons in init.go and accessed
// via package-level functions like sessionLLMClient().
type ChatAgent struct {
	sessionManager *SessionManager
	cookieName     string // cookie name for reading/writing sessionID

	// defaultLang is the default language for i18n (e.g., "zh-CN", "en").
	// Used for translating system prompts, tool descriptions, and other
	// content sent to the AI API and frontend.
	defaultLang string

	// avatarDir is the filesystem path to the avatar image directory.
	// Used by OnLogin to dynamically discover available avatar files.
	avatarDir string

	// smsCodeCache is the Redis-backed SMS verification code cache.
	// If nil, SMS code functionality is unavailable (Redis not configured).
	smsCodeCache *cache.SMSCodeCache

	logger zylog.Logger // Structured logger for the agent
}

// LLMInfo is the response for the LLM info endpoint.
type LLMInfo struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Website string `json:"website"`
}

// OnGetLLMInfo handles GET /api/info/llm/chat requests.
// Returns the current session's LLM provider name, model name, and official website URL as JSON.
func (h *ChatAgent) OnGetLLMInfo(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)
	client := sessionLLMClient(session)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LLMInfo{
		Name:    client.Name(),
		Model:   client.Model(),
		Website: client.Website(),
	})
}

// sessionLLMClient returns the LLM client for the user's configured provider.
// Falls back to the default provider (DeepSeek) if the user hasn't set one
// or if the configured provider is not in the registry.
func sessionLLMClient(s *session) llm.Client {
	provider := s.user.settings.APIKey.LLM.Provider
	if provider == "" {
		provider = ProviderDeepSeek
	}
	if c, ok := llmClients[provider]; ok {
		return c
	}
	return llmClients[ProviderDeepSeek]
}

// sessionLLMAPIKey returns the session user's personal LLM API key,
// or empty string if the user has not set one (meaning use the global default).
func sessionLLMAPIKey(s *session) string {
	return s.user.settings.APIKey.LLM.ApiKey
}

// sessionEmbedder returns the embedder for the user's configured provider.
func sessionEmbedder(s *session) embedder.Embedder {
	provider := s.user.settings.APIKey.Embedder.Provider
	if provider == "" {
		provider = ProviderAli
	}
	if e, ok := embedderClients[provider]; ok {
		return e
	}
	return embedderClients[ProviderAli]
}

// sessionEmbedderAPIKey returns the session user's personal Embedder API key,
// or empty string if not set (meaning use the global default).
func sessionEmbedderAPIKey(s *session) string {
	return s.user.settings.APIKey.Embedder.ApiKey
}

// sessionWebSearchAPIKey returns the session user's personal Web Search API key,
// or empty string if not set (meaning use the global default).
func sessionWebSearchAPIKey(s *session) string {
	return s.user.settings.APIKey.Search.ApiKey
}

// sessionWebSearcher returns the web search client for the user's configured provider.
// Returns nil if no provider is configured or the provider is not in the registry.
func sessionWebSearcher(s *session) toolimp.WebSearcher {
	provider := s.user.settings.APIKey.Search.Provider
	if provider == "" {
		return webSearchClients[ProviderBocha] // fallback to default
	}
	if w, ok := webSearchClients[provider]; ok {
		return w
	}
	return webSearchClients[ProviderBocha]
}

// SetRedisStore attaches a Redis session store to the ChatAgent's SessionManager.
// Must be called before handling requests if Redis is available.
// If nil, session management falls back to pure in-memory mode.
func (h *ChatAgent) SetRedisStore(redisStore *cache.RedisSessionStore) {
	h.sessionManager.SetRedisStore(redisStore)
}

// SetSMSCodeCache attaches a Redis-backed SMS code cache to the ChatAgent.
// If nil, SMS code functionality is unavailable.
func (h *ChatAgent) SetSMSCodeCache(c *cache.SMSCodeCache) {
	h.smsCodeCache = c
}

// Close releases all underlying resources held by the ChatAgent.
func (h *ChatAgent) Close() error {
	h.sessionManager.Close()
	return nil
}

// NewChatHandler creates a ChatAgent.
//
// cookieName: the cookie name for reading/writing sessionID, e.g. "brain_go_session"
// defaultLang: the default language for i18n, e.g. "zh-CN", "en". Empty string defaults to "en".
//
// Note: API clients are not passed here. They are initialized as package-level
// singletons in InitAgent and accessed via sessionLLMClient(), sessionEmbedder(),
// and sessionWebSearcher().
func NewChatHandler(
	cookieName string,
	defaultLang string,
	avatarDir string,
	logger zylog.Logger,
) *ChatAgent {
	if defaultLang == "" {
		defaultLang = "en"
	}
	return &ChatAgent{
		sessionManager: NewSessionManager(),
		cookieName:     cookieName,
		defaultLang:    defaultLang,
		avatarDir:      avatarDir,
		logger:         logger,
	}
}

// ============================================================
// Database connection helpers (via dbcfg global)
// ============================================================

// resolveUserSN returns the session's userSN (no fallback).
func resolveUserSN(s *session) string {
	return s.user.SN
}

// resolveUserID returns the session's userID (0 = anonymous).
func resolveUserID(s *session) int64 {
	return s.user.ID
}

// openChatDB opens the user's chat database (no schema check, for performance).
// Schema is ensured by dbc.InitUserDB() called during login.
// Caller MUST call closeChatDB when done.
func (h *ChatAgent) openChatDB(s *session) (*store.ChatStore, error) {
	return dbc.OpenLocalChatDB(resolveUserID(s), resolveUserSN(s))
}

// openBrainDB opens the user's brain database (no schema check).
// Caller MUST call closeBrainDB when done.
func (h *ChatAgent) openBrainDB(s *session) (*store.BrainStore, error) {
	return dbc.OpenLocalBrainDB(resolveUserID(s), resolveUserSN(s))
}

// closeChatDB safely closes a ChatStore (nil-safe).
func (h *ChatAgent) closeChatDB(cs *store.ChatStore) {
	dbc.CloseLocalChatDB(cs)
}

// closeBrainDB safely closes a BrainStore (nil-safe).
func (h *ChatAgent) closeBrainDB(vs *store.BrainStore) {
	dbc.CloseLocalBrainDB(vs)
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
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
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
	chatID := session.user.currentChat.dbChat.ID
	session.mu.Unlock()

	var msgs []Message
	if chatID != 0 {
		chatStore, cerr := h.openChatDB(session)
		if cerr != nil {
			http.Error(w, i18n.T("api_error_failed_to_open_chat_store_detail", map[string]any{"Error": cerr.Error()}), http.StatusInternalServerError)
			return
		}

		dbMessages, err := chatStore.ListMessages(chatID)
		if err != nil {
			h.closeChatDB(chatStore)
			http.Error(w, i18n.T("api_error_failed_to_list_messages", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
			return
		}
		agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, chatStore, chatID)
		h.closeChatDB(chatStore)
		if convErr != nil {
			http.Error(w, i18n.T("api_error_failed_to_load_web_sources", map[string]any{"Error": convErr.Error()}), http.StatusInternalServerError)
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
	title := session.user.currentChat.title
	titleState := int(session.user.currentChat.titleState)
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
// Uses chatsMu because it operates on session.user.chats (independent of streaming).
func (h *ChatAgent) OnChatPin(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	pinnedStr := r.URL.Query().Get("pinned")
	pinned := pinnedStr == "true"

	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	session.user.chatsMu.Lock()
	defer session.user.chatsMu.Unlock()

	// Find the session by SN
	var targetChat *store.Chat
	for i := range session.user.chats {
		if session.user.chats[i].SN == sn {
			targetChat = &session.user.chats[i]
			break
		}
	}
	if targetChat == nil {
		http.Error(w, i18n.T("db_session_not_found"), http.StatusNotFound)
		return
	}

	chatStore, cerr := h.openChatDB(session)
	if cerr != nil {
		http.Error(w, i18n.T("api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}

	if err := chatStore.UpdateChatPin(targetChat.ID, pinned); err != nil {
		h.closeChatDB(chatStore)
		http.Error(w, i18n.T("db_update_chat_pin_failed"), http.StatusInternalServerError)
		return
	}
	h.closeChatDB(chatStore)

	// Update in-memory cache
	targetChat.Pinned = pinned

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
