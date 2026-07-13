package agent

import "BrainForever/internal/agent/llmtypes"

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
