package agent

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/embedder"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/infra/zylog"
	"BrainForever/internal/agent/llmtypes"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/config"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
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
	sess := h.sessionManager.GetOrCreate(sessionID)

	// Phase 1: Find the chat by SN and remove from in-memory list (under ChatsMu lock)
	var chatID int64
	sess.User.ChatsMu.Lock()

	var found bool
	for i := range sess.User.Chats {
		if sess.User.Chats[i].SN == sn {
			chatID = sess.User.Chats[i].ID
			// Remove from in-memory list (normal chats)
			sess.User.Chats = append(sess.User.Chats[:i], sess.User.Chats[i+1:]...)
			found = true
			break
		}
	}
	sess.User.ChatsMu.Unlock()

	if !found {
		http.Error(w, i18n.T("db_session_not_found"), http.StatusNotFound)
		return
	}

	if chatID <= 0 {
		http.Error(w, i18n.T("api_error_validation_failed", map[string]any{"Error": "invalid chat ID"}), http.StatusInternalServerError)
		return
	}

	// Phase 2: If the deleted chat is the current active chat, reset it (under Mu lock)
	sess.Mu.Lock()
	if sess.User.CurrentChat != nil && sess.User.CurrentChat.DBCHat != nil && sess.User.CurrentChat.DBCHat.ID == chatID {
		sess.User.CurrentChat = &llmtypes.Chat{}
	}
	sess.Mu.Unlock()

	// Phase 3: Soft-delete (logic delete) -move to trash
	if err := theChatStore.LogicDelete(sn); err != nil {
		http.Error(w, i18n.T("api_error_failed_to_delete_session"), http.StatusInternalServerError)
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
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	deletedChats, err := theChatStore.ListDeletedChats(sess.User.ID, 100)
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
	sess := h.sessionManager.GetOrCreate(sessionID)

	// Restore in DB
	if err := theChatStore.RestoreChat(sn); err != nil {
		http.Error(w, i18n.T("db_restore_chat_failed"), http.StatusInternalServerError)
		return
	}

	// Reload the restored chat from DB and add back to in-memory list
	chats, err := theChatStore.ListChats(sess.User.ID, 100)
	if err == nil {
		sess.User.ChatsMu.Lock()
		sess.User.Chats = chats
		sess.User.ChatsMu.Unlock()
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

	chat, err := theChatStore.FindChatBySN(sn)
	if err != nil {
		http.Error(w, i18n.T("db_session_not_found"), http.StatusNotFound)
		return
	}

	chatID := chat.ID

	// Phase 1: Delete traits from brain DB (cross-table) BEFORE deleting the chat.
	if _, err := theBrainStore.DeleteByChatSN(sn); err != nil {
		h.logger.Errorf("failed to delete traits for chat %s: %v", sn, err)
	}

	// Phase 2: Delete the chat session from chats DB.
	if err := theChatStore.PhysicalDelete(int(chatID)); err != nil {
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
func (h *ChatAgent) OnEmptyTrash(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	deletedChats, err := theChatStore.ListDeletedChats(sess.User.ID, 1000)
	if err != nil {
		http.Error(w, i18n.T("db_list_deleted_chats_failed"), http.StatusInternalServerError)
		return
	}

	sns := make([]string, 0, len(deletedChats))
	for _, c := range deletedChats {
		if c.SN != "" {
			sns = append(sns, c.SN)
		}
	}
	if len(sns) > 0 {
		if _, err := theBrainStore.DeleteTraitsByChatSNs(sns); err != nil {
			h.logger.Errorf("failed to delete traits for trashed chats: %v", err)
		}
	}

	if err := theChatStore.EmptyTrash(sess.User.ID); err != nil {
		http.Error(w, i18n.T("db_delete_trashed_sessions_failed"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// ============================================================
// ChatAgent -POST /api/chat handler (core)
// ============================================================

// ChatAgent handles chat requests, integrating RAG retrieval + LLM streaming
type ChatAgent struct {
	sessionManager *session.Manager
	cookieName     string

	defaultLang string
	avatarDir   string

	smsCodeCache *cache.SMSCodeCache

	logger zylog.Logger
}

// LLMInfo is the response for the LLM info endpoint.
type LLMInfo struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Website string `json:"website"`
}

// OnGetLLMInfo handles GET /api/info/llm/chat requests.
func (h *ChatAgent) OnGetLLMInfo(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)
	client := sessionLLMClient(sess)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LLMInfo{
		Name:    client.Name(),
		Model:   client.Model(),
		Website: client.Website(),
	})
}

// sessionLLMClient returns the LLM client for the user's configured provider.
func sessionLLMClient(s *session.Session) llm.Client {
	provider := s.User.Settings.APIKey.LLM.Provider
	if provider == "" {
		provider = ProviderDeepSeek
	}
	if c, ok := llmClients[provider]; ok {
		return c
	}
	return llmClients[ProviderDeepSeek]
}

// sessionLLMApiSetting returns the session user's personal LLM ApiSetting.
func sessionLLMApiSetting(s *session.Session) store.ApiSetting {
	return s.User.Settings.APIKey.LLM
}

// sessionEmbedder returns the embedder for the user's configured provider.
func sessionEmbedder(s *session.Session) embedder.Embedder {
	provider := s.User.Settings.APIKey.Embedder.Provider
	if provider == "" {
		provider = config.GetDefaultEmbeddingProvider()
	}
	if e, ok := embedderClients[provider]; ok {
		return e
	}
	return embedderClients[config.GetDefaultEmbeddingProvider()]
}

// sessionEmbedderApiSetting returns the session user's personal Embedder ApiSetting.
func sessionEmbedderApiSetting(s *session.Session) store.ApiSetting {
	return s.User.Settings.APIKey.Embedder
}

// sessionWebSearchApiSetting returns the session user's personal Web Search ApiSetting.
func sessionWebSearchApiSetting(s *session.Session) store.ApiSetting {
	return s.User.Settings.APIKey.Search
}

// sessionWebSearcher returns a per-request webSearchAdapter with the
// user's configured provider and personal API settings baked in.
func sessionWebSearcher(s *session.Session) toolimp.WebSearcher {
	provider := s.User.Settings.APIKey.Search.Provider
	if provider == "" {
		provider = config.GetDefaultWebSearchProvider()
	}
	rawClient, ok := searcherClientByPvd[provider]
	if !ok {
		rawClient = searcherClientByPvd[config.GetDefaultWebSearchProvider()]
	}
	apiSetting := s.User.Settings.APIKey.Search
	return &webSearchAdapter{
		client:     rawClient,
		apiSetting: apiSetting,
	}
}

// GetSessionManager returns the underlying session manager.
func (h *ChatAgent) GetSessionManager() *session.Manager {
	return h.sessionManager
}

// GetCookieName returns the cookie name used for session identification.
func (h *ChatAgent) GetCookieName() string {
	return h.cookieName
}

// GetLogger returns the logger instance.
func (h *ChatAgent) GetLogger() zylog.Logger {
	return h.logger
}

// GetAvatarDir returns the avatar directory path.
func (h *ChatAgent) GetAvatarDir() string {
	return h.avatarDir
}

// GetSMSCodeCache returns the SMS code cache, or nil if not configured.
func (h *ChatAgent) GetSMSCodeCache() *cache.SMSCodeCache {
	return h.smsCodeCache
}

// SetRedisStore attaches a Redis session store to the ChatAgent's Manager.
func (h *ChatAgent) SetRedisStore(redisStore *cache.RedisSessionStore) {
	h.sessionManager.SetRedisStore(redisStore)
}

// SetSMSCodeCache attaches a Redis-backed SMS code cache to the ChatAgent.
func (h *ChatAgent) SetSMSCodeCache(c *cache.SMSCodeCache) {
	h.smsCodeCache = c
}

// Close releases all underlying resources held by the ChatAgent.
func (h *ChatAgent) Close() error {
	h.sessionManager.Close()
	return nil
}

// NewChatHandler creates a ChatAgent.
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
		sessionManager: session.NewManager(logger),
		cookieName:     cookieName,
		defaultLang:    defaultLang,
		avatarDir:      avatarDir,
		logger:         zylog.WrapWithSubject(logger, "agent"),
	}
}

// ============================================================
// Database connection helpers (via dbcfg global)
// ============================================================

func resolveUserSN(s *session.Session) string {
	return s.User.SN
}

func resolveUserID(s *session.Session) int64 {
	return s.User.ID
}

// ============================================================
// SwitchChat handler -GET /api/chat/switch?sn=XXX
// ============================================================

// OnSwitchChat handles GET /api/chat/switch -switches the current
// active chat to a historical chat identified by its SN, loading
// its messages from the database.
func (h *ChatAgent) OnSwitchChat(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	if err := sess.SwitchToChat(sn); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	sess.Mu.Lock()
	chatID := sess.User.CurrentChat.DBCHat.ID
	sess.Mu.Unlock()

	var msgs []Message
	if chatID != 0 {
		dbMessages, err := theChatStore.ListMessages(chatID)
		if err != nil {
			http.Error(w, i18n.T("api_error_failed_to_list_messages", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
			return
		}
		agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, theChatStore, chatID)
		if convErr != nil {
			http.Error(w, i18n.T("api_error_failed_to_load_web_sources", map[string]any{"Error": convErr.Error()}), http.StatusInternalServerError)
			return
		}
		msgs = agentMsgs
	}
	if msgs == nil {
		msgs = []Message{}
	}

	msgs = ensureAssistantForOrphanUser(msgs, lang)

	sess.Mu.Lock()
	title := sess.User.CurrentChat.Title
	titleState := int(sess.User.CurrentChat.TitleState)
	sess.Mu.Unlock()

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
// ============================================================

// OnChatPin handles PUT /api/chat/pin -toggles the pinned state of a chat.
func (h *ChatAgent) OnChatPin(w http.ResponseWriter, r *http.Request) {
	sn := r.URL.Query().Get("sn")
	if sn == "" {
		http.Error(w, i18n.T("api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	pinnedStr := r.URL.Query().Get("pinned")
	pinned := pinnedStr == "true"

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	sess.User.ChatsMu.Lock()
	defer sess.User.ChatsMu.Unlock()

	var targetChat *store.Chat
	for i := range sess.User.Chats {
		if sess.User.Chats[i].SN == sn {
			targetChat = &sess.User.Chats[i]
			break
		}
	}
	if targetChat == nil {
		http.Error(w, i18n.T("db_session_not_found"), http.StatusNotFound)
		return
	}

	if err := theChatStore.UpdateChatPin(targetChat.ID, pinned); err != nil {
		http.Error(w, i18n.T("db_update_chat_pin_failed"), http.StatusInternalServerError)
		return
	}

	targetChat.Pinned = pinned

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

// getSessionID gets the sessionID from the request
func (h *ChatAgent) getSessionID(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(h.cookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value, false
	}

	sessionID := session.GenerateSessionID()

	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7,
	})

	return sessionID, true
}

// refreshSession writes the given sessionID into the cookie with a fresh MaxAge.
func (h *ChatAgent) refreshSession(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7,
	})
}

// resolveSessionID is a convenience wrapper around getSessionID that discards the isNew flag.
func (h *ChatAgent) resolveSessionID(w http.ResponseWriter, r *http.Request) string {
	sessionID, _ := h.getSessionID(w, r)
	return sessionID
}
