package agent

import (
	"BrainForever/infra/llm"
	"BrainForever/internal/local/store"
	"BrainForever/toolset"
)

// ============================================================
// DB Utilities -session & message persistence
// ============================================================

// generateSessionSN generates a globally unique serial number for a chat session.
// Format: chat-<uuid-v4>
// Delegates to toolset.GenerateSN with prefix "chat".
func generateSessionSN() string {
	return toolset.GenerateSN("chat")
}

// ensureSessionDBForChat ensures that the current chat has a corresponding record
// in the chat_sessions table. If dbChat.ID is 0, it creates a new session
// record and sets dbChat.
// Must be called with session.mu held.
// Returns true if a new DB session was created, false if one already existed.
func ensureSessionDBForChat(session *session) bool {
	if session.currentChat.dbChat != nil && session.currentChat.dbChat.ID != 0 {
		return false // Already has a DB session
	}

	sn := generateSessionSN()
	title := session.currentChat.title

	dbChat, err := session.chatsStore.InsertChat(sn, 0, title, 0)
	if err != nil {
		return false
	}

	session.currentChat.dbChat = dbChat

	// Add the new chat to the in-memory list so it immediately appears
	// in the left sidebar's chat list (without requiring a page refresh).
	// NOTE: addChatToList locks chatsMu internally and is safe to call
	// while session.mu is held (no reverse lock ordering exists in the codebase).
	session.addChatToList(*dbChat)
	return true
}

// persistMessageToDB inserts a single message into the chat_messages table.
//
// After insertion, it also updates the chat session's update_at timestamp
// (via TouchChat) and moves the chat to the front of the in-memory list
// so active chats float to the top of the sidebar.
// Must be called with session.mu held.
//
// chatSN parameter is passed explicitly to avoid race conditions:
// session.currentChat may have been changed by OnSwitchChat while
// streaming was in progress (session.mu is NOT held during streaming).
func persistMessageToDB(session *session, msg *Message, chatSN string) {
	if chatSN == "" {
		return
	}

	// Map agent.Message role to store.Message role: 0=user, 1=assistant
	var role int
	switch msg.Role {
	case llm.RoleUser:
		role = 0
	case llm.RoleAssistant:
		role = 1
	default:
		return // Skip system messages
	}

	// Map agent.Message.ID (group index) to store.Message.GroupIndex
	groupIndex := int(msg.ID)

	// Map agent.Message.Reasoning to store.Message.Reasoning (*string)
	var reasoning *string
	if msg.Reasoning != "" {
		reasoning = &msg.Reasoning
	}

	if err := session.chatsStore.InsertMessage(
		chatSN,
		groupIndex,
		role,
		msg.Content,
		reasoning,
		msg.Interrupted,
	); err != nil {
		return
	}

	// Persist WebSources if present (assistant messages with web search results)
	if len(msg.Sources) > 0 {
		storeSources := make([]store.WebSource, 0, len(msg.Sources))
		for _, src := range msg.Sources {
			storeSources = append(storeSources, store.WebSource{
				ChatSN:      chatSN,
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
		session.chatsStore.InsertWebSources(chatSN, msg.ID, storeSources)
	}

	// Also move the chat to the front of the in-memory list so that
	// subsequent GET /api/session calls return the correct order.
	// This is safe: addChatToList also locks chatsMu while session.mu is held.
	//
	// WARNING: Never use nested append like:
	//   append(session.chats[:i], session.chats[i+1:]...)
	// session.chats[:i] shares the same underlying array as session.chats.
	// When there's spare capacity, the inner append mutates the shared array,
	// corrupting session.chats (producing duplicate entries with same ID/SN).
	session.chatsMu.Lock()
	for i, c := range session.chats {
		if c.SN == chatSN {
			// Touch the chat session's update_at
			session.chatsStore.TouchChat(c.ID)

			// Safe removal: copy all elements except index i into a new slice
			removed := session.chats[i]
			rest := make([]store.Chat, 0, len(session.chats)-1)
			rest = append(rest, session.chats[:i]...)
			rest = append(rest, session.chats[i+1:]...)
			// Prepend the removed element
			session.chats = append([]store.Chat{removed}, rest...)
			break
		}
	}
	session.chatsMu.Unlock()
}

// deduplicateChats removes duplicate entries from the in-memory chat list
// by keeping only the first occurrence of each unique ID.
// This is a safety net for any edge cases that might produce duplicates;
// the primary fix is the safe slice manipulation in persistMessageToDB.
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
