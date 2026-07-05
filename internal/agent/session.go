package agent

import (
	"fmt"
	"sync"
	"time"

	"BrainForever/internal/store"
	"BrainForever/toolset"
	"net/http"
	"sync/atomic"
)

// ============================================================
// Session types
// ============================================================

// sessionUser holds per-user state within a session.
// Zero value = anonymous (not logged in).
type sessionUser struct {
	ID          int64        // User's database ID (0 = not logged in)
	SN          string       // User serial number; empty = not logged in
	chatsMu     sync.Mutex   // Protects: chats
	chats       []store.Chat // User's chat list from the database
	currentChat *chat        // Current active chat (messages, title, titleState)
}

// session represents an individual user's session
type session struct {
	mu sync.Mutex // protects: user, lastActivity

	lastActivity time.Time // Last activity time, used by GC for cleanup

	id   string      // HTTP cookie session ID
	user sessionUser // Logged-in user identity (zero value = anonymous)
}

// GetTitle returns the current title and its modification state atomically.
func (s *session) GetTitle() (string, TitleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.user.currentChat.title, s.user.currentChat.titleState
}

// SetTitle sets both the title and its modification state atomically.
// Title is always updated. TitleState only moves forward (0->, 0->, 1->).
func (s *session) SetTitle(newTitle string, newState TitleState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.user.currentChat.title = newTitle
	if newState > s.user.currentChat.titleState {
		s.user.currentChat.titleState = newState
	}
}

// switchToUser sets the session's user state.
// For login: id>0, sn is non-empty, chats are pre-loaded by the caller via dbc.InitUserDB.
// For logout: id=0, sn is empty, chats is nil (clears session).
func (s *session) switchToUser(id int64, sn string, chats []store.Chat) {
	if chats == nil {
		chats = []store.Chat{}
	}
	s.user.chatsMu.Lock()
	s.user.chats = chats
	s.user.chatsMu.Unlock()

	s.mu.Lock()
	s.user.currentChat = &chat{}
	s.user = sessionUser{ID: id, SN: sn, chats: chats, currentChat: &chat{}}
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
	s.user.currentChat = &chat{
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
	s.user.chatsMu.Lock()
	defer s.user.chatsMu.Unlock()

	for i := range s.user.chats {
		if s.user.chats[i].SN == sn {
			return &s.user.chats[i]
		}
	}
	return nil
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
	return s.user.currentChat == nil || s.user.currentChat.dbChat == nil || s.user.currentChat.dbChat.SN == ""
}

// IsAnonymous returns true if the session has no logged-in user.
// An anonymous session has user.ID == 0 (the zero value of sessionUser).
func (s *session) IsAnonymous() bool {
	return s.user.ID == 0
}

// addChatToList adds a store.Chat to the in-memory chat list (session.chats)
// if it's not already present. Must be called with session.mu NOT held
// (it locks chatsMu internally).
// This is called from ensureDBSession after creating a new DB chat record,
// so that the new chat immediately appears in the left sidebar's chat list.
func (s *session) addChatToList(chat store.Chat) {
	s.user.chatsMu.Lock()
	defer s.user.chatsMu.Unlock()

	// Avoid duplicates
	for _, c := range s.user.chats {
		if c.SN == chat.SN {
			return
		}
	}

	// Prepend to list (newest first)
	s.user.chats = append([]store.Chat{chat}, s.user.chats...)
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
	if s.user.currentChat.dbChat == nil {
		s.mu.Unlock()
		return
	}
	chatID := s.user.currentChat.dbChat.ID
	s.mu.Unlock()

	if chatID == 0 {
		return
	}

	s.user.chatsMu.Lock()
	defer s.user.chatsMu.Unlock()
	for i := range s.user.chats {
		if s.user.chats[i].ID == chatID {
			s.user.chats[i].Title = title
			s.user.chats[i].TitleState = int8(titleState)
			return
		}
	}
}

// ============================================================
// Session ID generation & resolution
// ============================================================

// sessionAutoIncID provides thread-safe auto-increment for session ID generation
var sessionAutoIncID atomic.Uint64

// generateSessionID generates a locally unique HTTP session ID.
// Only needs local uniqueness (single-server scope), so uses the lightweight
// GenerateSNSimple (UUID v4) rather than the three-factor GenerateSN.
// Format: s-xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
func generateSessionID() string {
	return toolset.GenerateSNSimple("s")
}

// getSessionID gets the sessionID from the request
// Prefers reading from cookie; if absent, generates a new UUID and writes it to cookie
// Returns the sessionID and a bool indicating whether this is a newly created session
func (h *ChatAgent) getSessionID(w http.ResponseWriter, r *http.Request) (string, bool) {
	// Try to read from cookie
	cookie, err := r.Cookie(h.cookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value, false
	}

	// No cookie, generate a new sessionID
	sessionID := generateSessionID()

	// Write cookie (HttpOnly prevents XSS access, Path=/ makes it effective for all paths)
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // Expires in 7 days
	})

	return sessionID, true
}

// refreshSession writes the given sessionID into the cookie with a fresh MaxAge,
// effectively refreshing the session cookie expiry without generating a new ID.
func (h *ChatAgent) refreshSession(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // Expires in 7 days
	})
}

// resolveSessionID is a convenience wrapper around getSessionID that discards the isNew flag.
// Use this when you only need the sessionID string and don't care whether it's new.
func (h *ChatAgent) resolveSessionID(w http.ResponseWriter, r *http.Request) string {
	sessionID, _ := h.getSessionID(w, r)
	return sessionID
}
