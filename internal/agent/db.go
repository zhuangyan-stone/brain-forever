package agent

import (
	"BrainForever/infra/llm"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/toolset"
)

// ============================================================
// DB Utilities -session & message persistence
// ============================================================

// generateSessionSN generates a globally unique serial number for a chat session.
func generateSessionSN() string {
	return toolset.GenerateSN("chat")
}

// ensureSessionDBForChat ensures that the current chat has a corresponding record
// in the chat_sessions table. If DBCHat.ID is 0, it creates a new session
// record and sets DBCHat.
// Must be called with Mu held.
// Returns true if a new DB session was created, false if one already existed.
func ensureSessionDBForChat(sess *session.Session, chatStore *store.ChatStore) bool {
	if sess.User.CurrentChat.DBCHat != nil && sess.User.CurrentChat.DBCHat.ID != 0 {
		return false // Already has a DB session
	}

	sn := generateSessionSN()
	title := sess.User.CurrentChat.Title

	dbChat, err := chatStore.InsertChat(sn, 0, title, 0)
	if err != nil {
		return false
	}

	sess.User.CurrentChat.DBCHat = dbChat

	// Add the new chat to the in-memory list so it immediately appears
	// in the left sidebar's chat list.
	sess.AddChatToList(*dbChat)
	return true
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

// deduplicateChats removes duplicate entries from the in-memory chat list
func deduplicateChats(chats []store.Chat) []store.Chat {
	seen := make(map[int64]bool, len(chats))
	result := make([]store.Chat, 0, len(chats))
	for _, c := range chats {
		if !seen[c.ID] {
			seen[c.ID] = true
			result = append(result, c)
		}
	}
	return result
}
