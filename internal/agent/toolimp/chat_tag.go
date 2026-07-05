package toolimp

import (
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"encoding/json"
	"fmt"
)

// ============================================================
// Chat Tag Tool -LLM function-calling for conversation topic classification
// ============================================================

// ChatTagToolName is the name of the tool used for chat topic tagging.
// The LLM can call this tool when it needs to classify a conversation
// into topic categories based on the chat title and content.
const ChatTagToolName = "chat_tag"

// TagItem represents a single tag string.
type TagItem struct {
	Tag string `json:"tag"`
}

// chatTagToolDefinition returns the ToolDefinition for chat topic tagging
// using llm types, with translated descriptions.
//
// The tool expects the LLM to provide a "tags" parameter containing a JSON array
// of tag strings.
func chatTagToolDefinition(lang string) llm.ToolDefinition {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":        "string",
					"description": i18n.Tools.TL(lang, ChatTagToolName, "result_tag"),
				},
				"description": "Array of classification tag strings",
			},
		},
		"required":             []string{"tags"},
		"additionalProperties": false,
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal chat tag schema: %v", err))
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(schemaBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to parse chat tag schema: %v", err))
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        ChatTagToolName,
			Description: i18n.Tools.TL(lang, ChatTagToolName, "description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}

// ChatTagToolImp implements the llm.ToolIMP interface for chat topic tagging.
// When the LLM calls this tool, it provides the classification as arguments,
// and the tool validates and stores the result.
type ChatTagToolImp struct {
	def  llm.ToolDefinition
	lang string

	// Tags holds the parsed tag strings from the LLM's tool call arguments.
	Tags []string
}

// Ensure ChatTagToolImp implements llm.ToolIMP at compile time.
var _ llm.ToolIMP = (*ChatTagToolImp)(nil)

// MakeChatTagTool creates a new ChatTagToolImp with the given language.
func MakeChatTagTool(lang string) *ChatTagToolImp {
	return &ChatTagToolImp{def: chatTagToolDefinition(lang), lang: lang}
}

func (f *ChatTagToolImp) GetName() string {
	return ChatTagToolName
}

func (f *ChatTagToolImp) GetDefinition() llm.ToolDefinition {
	return f.def
}

// SetArgument parses the LLM's tool call arguments JSON.
// Expected format:
//
//	{"tags": ["tag1", "tag2", ...]}
func (f *ChatTagToolImp) SetArgument(arguments string) error {
	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return fmt.Errorf("failed to parse chat tag arguments: %w", err)
	}
	f.Tags = result.Tags
	return nil
}

func (f *ChatTagToolImp) GetPendingText() string {
	return i18n.Tools.TL(f.lang, ChatTagToolName, "pending")
}

// Execute returns the tag classification result as a formatted string.
// This result is sent back to the LLM as the tool response.
func (f *ChatTagToolImp) Execute() (string, error) {
	if len(f.Tags) == 0 {
		return "", fmt.Errorf("no tags provided")
	}

	result := "Topic classification results:\n"
	for _, t := range f.Tags {
		result += fmt.Sprintf("- %s\n", t)
	}
	return result, nil
}
