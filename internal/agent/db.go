package agent

import (
	"log"

	"BrainForever/infra/llm"
	"BrainForever/toolset"
)

// ============================================================
// DB Utilities — session & message persistence
// ============================================================

// generateSessionSN generates a globally unique serial number for a chat session.
// Format: chat-<uuid-v4>
// Delegates to toolset.GenerateSN with prefix "chat".
func generateSessionSN() string {
	return toolset.GenerateSN("chat")
}

// ensureDBSession ensures that the current chat has a corresponding record
// in the chat_sessions table. If dbSessionID is 0, it creates a new session
// record and sets dbSessionID.
//
// Exception: anonymous users (chatStore == nil) have no DB persistence,
// so no DB session record is created.
// Must be called with session.mu held.
func ensureDBSession(session *session) {
	if session.chatStore == nil {
		return // Anonymous user, no DB persistence
	}
	if session.getDbSessionIDWithoutLock() != 0 {
		return // Already has a DB session
	}

	sn := generateSessionSN()
	title, _ := session.getTitleWithoutLock()

	dbChat, err := session.chatStore.InsertChat(sn, 0, title, 0)
	if err != nil {
		log.Printf("failed to insert DB chat for user %s: %v", session.userNo, err)
		return
	}

	session.setDbSessionIDWithoutLock(dbChat.ID)
}

// persistMessageToDB inserts a single message into the chat_messages table.
//
// Exception: anonymous users (chatStore == nil) have no DB persistence,
// so messages are not stored in the database.
// Must be called with session.mu held.
func persistMessageToDB(session *session, msg *Message) {
	if session.chatStore == nil {
		return // Anonymous user, no DB persistence
	}
	if session.getDbSessionIDWithoutLock() == 0 {
		log.Printf("cannot persist message: no DB session ID for user %s", session.userNo)
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

	if err := session.chatStore.InsertMessage(
		session.getDbSessionIDWithoutLock(),
		groupIndex,
		role,
		msg.Content,
		reasoning,
	); err != nil {
		log.Printf("failed to persist message to DB for user %s: %v", session.userNo, err)
	}
}
