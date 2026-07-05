package agent

import "BrainForever/internal/store"

// TitleState represents the state of the session title modification.
//
//	0: original title (default, "New Chat" for new sessions)
//	1: AI-modified title
//	2: user-modified title
type TitleState int

const (
	TitleStateOriginal     TitleState = iota // 0: original title
	TitleStateAIModified                     // 1: AI-modified title
	TitleStateUserModified                   // 2: user-modified title
)

type chat struct {
	dbChat *store.Chat // Bridge to store.Chat (never nil after creation)

	title      string     // Session title, generated from the first user message content
	titleState TitleState // Title modification state
}
