package toolimp

import (
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/store"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ============================================================
// Chat Samples Tool -LLM function-calling for fetching conversation samples
// ============================================================

// ChatSamplesToolName is the name of the tool used for fetching
// sample messages from the current conversation when the title
// alone is insufficient for topic classification.
// This is the name exposed to the LLM API.
const ChatSamplesToolName = "get_chat_samples_messages"

// chatSamplesI18NKey is the key used for i18n translation lookups.
// It matches the translation file name (without extension) in the
// tools/ directory and the key registered in i18n.TLTools.
const chatSamplesI18NKey = "chat_samples_messages"

// pageSize is the number of messages (user + assistant) loaded per tool call.
const pageSize = 10

// chatSamplesToolDefinition returns the ToolDefinition for fetching
// conversation message samples using llm types, with translated descriptions.
//
// This tool takes no parameters -the LLM simply calls it to get
// sample messages from the current conversation.
func chatSamplesToolDefinition(lang string) llm.ToolDefinition {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []string{},
		"additionalProperties": false,
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal chat samples schema: %v", err))
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(schemaBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to parse chat samples schema: %v", err))
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        ChatSamplesToolName,
			Description: i18n.Tools.TL(lang, chatSamplesI18NKey, "description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}

// ChatSamplesToolImp implements the llm.ToolIMP interface for fetching
// conversation message samples incrementally.
// Each call to Execute() loads up to pageSize (10) messages from the DB,
// starting from where the previous call left off.
type ChatSamplesToolImp struct {
	def  llm.ToolDefinition
	lang string

	// chatID is the ID of the target chat session, set at creation time.
	chatID int64

	// chatsStore is the store used to query messages from DB.
	chatsStore *store.ChatStore

	// nextStartMessageID is the message ID cursor for pagination.
	// Start at 0 so the first query loads messages with id > 0 (i.e., from the beginning).
	nextStartMessageID int64

	// allMessagesLoaded is set to true once all messages have been loaded.
	allMessagesLoaded bool

	// chatTitle is cached for formatting the output.
	chatTitle string

	// totalMessages is the total number of messages in this chat (set at creation time).
	totalMessages int

	// viewedMessageCount is the cumulative count of messages returned to the LLM
	// across all Execute() calls.
	viewedMessageCount int
}

// Ensure ChatSamplesToolImp implements llm.ToolIMP at compile time.
var _ llm.ToolIMP = (*ChatSamplesToolImp)(nil)

// MakeChatSamplesTool creates a new ChatSamplesToolImp with the given language,
// chat store reference, chat ID, and chat title.
// totalMessages is the total number of messages in this chat.
func MakeChatSamplesTool(lang string, chatsStore *store.ChatStore, chatID int64, chatTitle string, totalMessages int) *ChatSamplesToolImp {
	return &ChatSamplesToolImp{
		def:           chatSamplesToolDefinition(lang),
		lang:          lang,
		chatID:        chatID,
		chatsStore:    chatsStore,
		chatTitle:     chatTitle,
		totalMessages: totalMessages,
	}
}

// SetChatTitle updates the cached chat title (e.g., if the title was set after creation).
func (f *ChatSamplesToolImp) SetChatTitle(title string) {
	f.chatTitle = title
}

func (f *ChatSamplesToolImp) GetName() string {
	return ChatSamplesToolName
}

func (f *ChatSamplesToolImp) GetDefinition() llm.ToolDefinition {
	return f.def
}

func (f *ChatSamplesToolImp) SetArgument(arguments string) error {
	// This tool has no parameters, so no parsing is needed.
	return nil
}

// GetTotalMessages returns the total number of messages in the chat.
func (f *ChatSamplesToolImp) GetTotalMessages() int {
	return f.totalMessages
}

// GetViewedMessageCount returns how many messages have been viewed by the LLM so far.
func (f *ChatSamplesToolImp) GetViewedMessageCount() int {
	return f.viewedMessageCount
}

// IsAllMessagesViewed returns true if all messages in the chat have been loaded/viewed.
func (f *ChatSamplesToolImp) IsAllMessagesViewed() bool {
	return f.allMessagesLoaded
}

func (f *ChatSamplesToolImp) GetPendingText() string {
	return i18n.Tools.TL(f.lang, chatSamplesI18NKey, "pending")
}

// Execute loads the next batch of messages (up to pageSize=10) from the DB
// and returns them as formatted text for the LLM.
//
// First call: loads messages 1-10.
// Second call: loads messages 11-20.
// And so on until all messages are loaded.
// Once all messages are loaded, allMessagesLoaded is set to true and
// subsequent calls return a message indicating no more content.
func (f *ChatSamplesToolImp) Execute() (string, error) {
	// If all messages are already loaded, return a notification.
	if f.allMessagesLoaded {
		return i18n.Tools.TL(f.lang, chatSamplesI18NKey, "all_messages_loaded"), nil
	}

	if f.chatID == 0 {
		return "", fmt.Errorf("no valid chat ID available")
	}

	// Load the next batch of messages from DB using chatID.
	dbMessages, err := f.chatsStore.ListMessagesByRange(f.chatID, f.nextStartMessageID, pageSize)
	if err != nil {
		return "", fmt.Errorf("failed to load messages: %w", err)
	}

	if len(dbMessages) == 0 {
		f.allMessagesLoaded = true
		return i18n.Tools.TL(f.lang, chatSamplesI18NKey, "no_messages"), nil
	}

	// Check if we've loaded all remaining messages.
	if len(dbMessages) < pageSize {
		f.allMessagesLoaded = true
	}

	// Update the cursor to the last message's ID for next pagination.
	f.nextStartMessageID = dbMessages[len(dbMessages)-1].ID

	// Accumulate viewed message count.
	f.viewedMessageCount += len(dbMessages)

	// Format the messages for LLM consumption.
	var parts []string

	for _, m := range dbMessages {
		roleLabel := i18n.TL(f.lang, "label_user")
		if m.Role == 1 {
			roleLabel = i18n.TL(f.lang, "label_ai")
		}
		content := m.Content
		if utf8.RuneCountInString(content) > 500 {
			runes := []rune(content)
			content = string(runes[:500]) + "..."
		}
		parts = append(parts, roleLabel+content)
	}

	result := strings.Join(parts, "\n")
	return result, nil
}
