package agent

import (
	"BrainForever/infra/embedder"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/llmtypes"
	"BrainForever/internal/store"
)

// ============================================================
// Type aliases re-exported from llmtypes package
//
// These aliases allow existing code in the agent package to
// continue using the short names (chat, TitleState, Message, etc.)
// without requiring import path changes across all files.
// ============================================================

type TitleState = llmtypes.TitleState

const (
	TitleStateOriginal     = llmtypes.TitleStateOriginal
	TitleStateAIModified   = llmtypes.TitleStateAIModified
	TitleStateUserModified = llmtypes.TitleStateUserModified
)

type chat = llmtypes.Chat
type Message = llmtypes.Message
type ChatRequest = llmtypes.ChatRequest
type Usage = llmtypes.Usage

// SSE event types
type ReasoningEvent = llmtypes.ReasoningEvent
type ReasoningEndEvent = llmtypes.ReasoningEndEvent
type TextEvent = llmtypes.TextEvent
type WebSourceEvent = llmtypes.WebSourceEvent
type DoneEvent = llmtypes.DoneEvent
type ErrorEvent = llmtypes.ErrorEvent
type ChatCreatedEvent = llmtypes.ChatCreatedEvent

// Function references (lowercase to match original usage in agent package)
var convertDBMessagesToAgentMessages = llmtypes.ConvertDBMessagesToAgentMessages
var loadMessagesAsLLMMessages = llmtypes.LoadMessagesAsLLMMessages
var ensureAssistantForOrphanUser = llmtypes.EnsureAssistantForOrphanUser
var makeAssistantBrokenMessage = llmtypes.MakeAssistantBrokenMessage

// ============================================================
// Exported getters for package-level globals (used by tasks package)
// ============================================================

// GetChatStore returns the global ChatStore instance.
func GetChatStore() *store.ChatStore {
	return theChatStore
}

// GetBrainStore returns the global BrainStore instance.
func GetBrainStore() *store.BrainStore {
	return theBrainStore
}

// GetLLMClients returns the global LLM client map keyed by provider name.
func GetLLMClients() map[string]llm.Client {
	return llmClients
}

// GetEmbedderClients returns the global Embedder client map keyed by provider name.
func GetEmbedderClients() map[string]embedder.Embedder {
	return embedderClients
}
