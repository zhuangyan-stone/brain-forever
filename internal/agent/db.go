package agent

import (
	"BrainForever/infra/llm"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/toolset"
	"fmt"
	"strings"
)

// ============================================================
// DB Utilities -session & message persistence
// ============================================================

// generateSessionSN generates a globally unique serial number for a chat session.
func generateSessionSN() string {
	return toolset.GenerateSN("chat-")
}

// ensureSessionDBForChat ensures that the current chat has a corresponding record
// in the chat_sessions table. If DBCHat.ID is 0, it creates a new session
// record and sets DBCHat.
// Must be called with Mu held.
// Returns true if a new DB session was created, false if one already existed.
// Returns an error if the DB insert fails.
//
// frontSN is the SN sent by the frontend. For existing chats (real DB SN),
// if the session has lost its state (e.g. after backend restart), this function
// attempts to restore the existing chat from the database instead of creating
// a new one. Frontend temporary SNs (prefixed with "new_") are treated as new chats.
func ensureSessionDBForChat(sess *session.Session, chatStore *store.ChatStore, frontSN string) (bool, error) {
	if sess.User.CurrentChat.DBCHat != nil && sess.User.CurrentChat.DBCHat.ID != 0 {
		return false, nil // Already has a DB session
	}

	// ★ If front_sn is a real DB SN (not a frontend temp "new_" SN),
	//   try to restore the existing chat from the database.
	//   This handles the case where the backend restarted but the frontend
	//   still has a valid chat (reconnection scenario).
	if frontSN != "" && !strings.HasPrefix(frontSN, "new_") {
		dbChat, err := chatStore.FindChatBySN(frontSN)
		if err == nil && dbChat != nil {
			sess.User.CurrentChat.DBCHat = dbChat
			sess.User.CurrentChat.Title = dbChat.Title
			sess.User.CurrentChat.TitleState = TitleState(dbChat.TitleState)

			// Load messages from DB into the in-memory cache
			dbMessages, err := chatStore.ListMessages(dbChat.ID)
			if err == nil {
				agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, chatStore, dbChat.ID)
				if convErr == nil {
					sess.User.CurrentChat.Messages = agentMsgs
				}
			}
			return false, nil // Not a new chat - restored existing one
		}
		// If FindChatBySN fails (SN not found or different user), fall through
		// to create a new chat below.
	}

	sn := generateSessionSN()
	title := sess.User.CurrentChat.Title

	dbChat, err := chatStore.InsertChat(sn, sess.User.ID, 0, title, 0)
	if err != nil {
		return false, fmt.Errorf("failed to ensure DB session for chat. %w", err)
	}

	sess.User.CurrentChat.DBCHat = dbChat

	// Add the new chat to the in-memory list so it immediately appears
	// in the left sidebar's chat list.
	sess.AddChatToList(*dbChat)
	return true, nil
}

// persistMessageToDB inserts a single message into the chat_messages table.
// Must be called with Mu held.
//
// chatID parameter is passed explicitly to avoid race conditions:
// session.CurrentChat may have been changed by OnSwitchChat while
// streaming was in progress (Mu is NOT held during streaming).
func persistMessageToDB(sess *session.Session, msg *Message, chatID int64, chatStore *store.ChatStore) {
	if chatID == 0 {
		return
	}

	var role int
	switch msg.Role {
	case llm.RoleUser:
		role = 0
	case llm.RoleAssistant:
		role = 1
	default:
		return
	}

	groupIndex := int(msg.ID)

	var reasoning *string
	if msg.Reasoning != "" {
		reasoning = &msg.Reasoning
	}

	if err := chatStore.InsertMessage(
		chatID,
		groupIndex,
		role,
		msg.Content,
		reasoning,
		msg.Interrupted,
	); err != nil {
		return
	}

	// Persist WebSources if present
	if len(msg.Sources) > 0 {
		storeSources := make([]store.WebSource, 0, len(msg.Sources))
		for _, src := range msg.Sources {
			storeSources = append(storeSources, store.WebSource{
				ChatID:      chatID,
				MsgID:       msg.ID,
				Title:       src.Title,
				Content:     src.Content,
				URL:         src.URL,
				SiteName:    src.SiteName,
				SiteIcon:    src.SiteIcon,
				PublishDate: src.PublishDate,
				Score:       src.Score,
			})
		}
		chatStore.InsertWebSources(chatID, msg.ID, storeSources)
	}

	// Also move the chat to the front of the in-memory list
	sess.User.ChatsMu.Lock()
	for i, c := range sess.User.Chats {
		if c.ID == chatID {
			chatStore.TouchChat(c.ID)

			removed := sess.User.Chats[i]
			rest := make([]store.Chat, 0, len(sess.User.Chats)-1)
			rest = append(rest, sess.User.Chats[:i]...)
			rest = append(rest, sess.User.Chats[i+1:]...)
			sess.User.Chats = append([]store.Chat{removed}, rest...)
			break
		}
	}
	sess.User.ChatsMu.Unlock()
}
