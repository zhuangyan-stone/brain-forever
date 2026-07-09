package session

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"BrainForever/internal/agent/llmtypes"
	"BrainForever/internal/store"
	"BrainForever/toolset"
)

// ============================================================
// Session ID generation
// ============================================================

// GenerateSessionID generates a locally unique HTTP session ID.
// Only needs local uniqueness (single-server scope), so uses the lightweight
// GenerateSNSimple (UUID v4) rather than the three-factor GenerateSN.
// Format: s-xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
func GenerateSessionID() string {
	return toolset.GenerateSNSimple("s-")
}

// ResolveSessionID extracts the session ID from the cookie.
// If no cookie exists, generates a new session ID and sets the cookie.
func ResolveSessionID(w http.ResponseWriter, r *http.Request, cookieName string) string {
	cookie, err := r.Cookie(cookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	sessionID := GenerateSessionID()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7,
	})
	return sessionID
}

// ============================================================
// Session types
// ============================================================

// SessionUser holds per-user state within a session.
// Zero value = anonymous (not logged in).
type SessionUser struct {
	ID          int64          // User's database ID (0 = not logged in)
	SN          string         // User serial number; empty = not logged in
	No          string         // User display number (e.g. "U12345"); empty = not logged in
	Nickname    string         // User's nickname; empty = not logged in
	ChatsMu     sync.Mutex     // Protects: Chats
	Chats       []store.Chat   // User's chat list from the database
	CurrentChat *llmtypes.Chat // Current active chat (messages, title, titleState)

	Settings store.UserSettings // User's personal settings (API keys, theme, etc.)
}

// CopyTo copies data fields from src to dst, preserving dst's ChatsMu mutex.
// Both src and dst are pointers to avoid copying sync.Mutex.
func (src *SessionUser) CopyTo(dst *SessionUser) {
	dst.ID = src.ID
	dst.SN = src.SN
	dst.No = src.No
	dst.Nickname = src.Nickname
	dst.CurrentChat = src.CurrentChat
	dst.Settings = src.Settings

	// Copy Chats under dst's own ChatsMu lock (preserving the existing mutex)
	dst.ChatsMu.Lock()
	dst.Chats = src.Chats
	dst.ChatsMu.Unlock()
}

// Session represents an individual user's session
type Session struct {
	Mu sync.Mutex // protects: User, LastActivity

	LastActivity time.Time   // Last activity time, used by GC for cleanup
	ID           string      // HTTP cookie session ID
	User         SessionUser // Logged-in user identity (zero value = anonymous)
}

// ============================================================
// Session methods
// ============================================================

// GetTitle returns the current title and its modification state atomically.
func (s *Session) GetTitle() (string, llmtypes.TitleState) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.User.CurrentChat.Title, s.User.CurrentChat.TitleState
}

// SetTitle sets both the title and its modification state atomically.
// Title is always updated. TitleState only moves forward (0->1, 0->2, 1->2).
func (s *Session) SetTitle(newTitle string, newState llmtypes.TitleState) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.User.CurrentChat.Title = newTitle
	if newState > s.User.CurrentChat.TitleState {
		s.User.CurrentChat.TitleState = newState
	}
}

// SwitchToUser sets the session's user state.
func (s *Session) SwitchToUser(user *SessionUser) {
	if user.Chats == nil {
		user.Chats = []store.Chat{}
	}
	if user.CurrentChat == nil {
		user.CurrentChat = &llmtypes.Chat{}
	}

	s.Mu.Lock()
	user.CopyTo(&s.User)
	s.Mu.Unlock()
}

// SwitchToChat switches the current active chat to a historical session
// identified by its serial number (SN).
func (s *Session) SwitchToChat(sn string) error {
	foundChat := s.FindChatBySN(sn)
	if foundChat == nil {
		return fmt.Errorf("session not found: %s", sn)
	}

	s.Mu.Lock()
	s.User.CurrentChat = &llmtypes.Chat{
		DBCHat:     foundChat,
		Title:      foundChat.Title,
		TitleState: llmtypes.TitleState(foundChat.TitleState),
	}
	s.Mu.Unlock()

	return nil
}

// FindChatBySN finds a chat by its serial number (SN) in the session's chat list.
func (s *Session) FindChatBySN(sn string) *store.Chat {
	s.User.ChatsMu.Lock()
	defer s.User.ChatsMu.Unlock()

	for i := range s.User.Chats {
		if s.User.Chats[i].SN == sn {
			return &s.User.Chats[i]
		}
	}
	return nil
}

// IsBlankChat checks whether CurrentChat is a "blank chat".
// Must be called with Mu held.
func (s *Session) IsBlankChat() bool {
	return s.User.CurrentChat == nil || s.User.CurrentChat.DBCHat == nil || s.User.CurrentChat.DBCHat.SN == ""
}

// IsAnonymous returns true if the session has no logged-in user.
func (s *Session) IsAnonymous() bool {
	return s.User.ID == 0
}

// AddChatToList adds a store.Chat to the in-memory chat list (session.Chats).
func (s *Session) AddChatToList(chat store.Chat) {
	s.User.ChatsMu.Lock()
	defer s.User.ChatsMu.Unlock()

	for _, c := range s.User.Chats {
		if c.SN == chat.SN {
			return
		}
	}

	s.User.Chats = append([]store.Chat{chat}, s.User.Chats...)
}

// SyncCurrentChatTitleToChatList syncs the current chat's title back to the
// in-memory sess.Chats list.
func (s *Session) SyncCurrentChatTitleToChatList(title string, titleState int) {
	s.Mu.Lock()
	if s.User.CurrentChat.DBCHat == nil {
		s.Mu.Unlock()
		return
	}
	chatID := s.User.CurrentChat.DBCHat.ID
	s.Mu.Unlock()

	if chatID == 0 {
		return
	}

	s.User.ChatsMu.Lock()
	defer s.User.ChatsMu.Unlock()
	for i := range s.User.Chats {
		if s.User.Chats[i].ID == chatID {
			s.User.Chats[i].Title = title
			s.User.Chats[i].TitleState = int8(titleState)
			return
		}
	}
}
