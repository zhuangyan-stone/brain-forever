package agent

import (
	"context"
	"fmt"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
)

// ============================================================
// Excerpt result types
// ============================================================

// Max DB field lengths for excerpts (matching VARCHAR column definitions).
const (
	MaxExcerptTextLen    = 380 // excerpts.content VARCHAR(380)
	MaxContextSummaryLen = 520 // excerpts.context_summary VARCHAR(520)
	MaxReasonLen         = 400 // excerpts.reason VARCHAR(400)
)

// TruncateExcerptItem truncates all string fields of an ExcerptItem to fit
// within the DB column limits. This is the programmatic safety net after
// the LLM has been asked (via system prompt) to respect the limits.
func TruncateExcerptItem(item *ExcerptItem) {
	item.ExcerptText = truncateToRunes(item.ExcerptText, MaxExcerptTextLen)
	item.ContextSummary = truncateToRunes(item.ContextSummary, MaxContextSummaryLen)
	item.Reason = truncateToRunes(item.Reason, MaxReasonLen)
}

// truncateToRunes truncates a string to the given maximum number of runes.
// No ellipsis is appended (the DB will reject values that are still too long).
func truncateToRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// ExcerptItem represents a single excerpt entry from the LLM response.
type ExcerptItem struct {
	ExcerptText    string   `json:"excerpt_text"`
	ValueTypes     []string `json:"value_types"`
	ContextSummary string   `json:"context_summary"`
	Reason         string   `json:"reason"`
	MsgID          int64    `json:"msg_id"`
}

// ExcerptResult holds all excerpts extracted from a chat.
type ExcerptResult struct {
	Excerpts []ExcerptItem
}

// ============================================================
// System prompt
// ============================================================

// getExcerptSystemPrompt returns the [excerpt] system prompt with template variables filled.
func getExcerptSystemPrompt(lang string, chatTitle string) string {
	return i18n.SystemPrompt.TL(lang, "excerpt", map[string]any{
		"ChatTitle": chatTitle,
	})
}

// ============================================================
// Message builder
// ============================================================

// buildExcerptLLMMessages builds the LLM message list from DB messages.
//
// Format for user messages:
//
//	[99] message content
//
// Format for assistant messages (no [msg_id] tag):
//
//	message content
//
// Assistant messages longer than 1024 chars are truncated (first 500 + last 500).
func buildExcerptLLMMessages(title string, dbMessages []store.Message, lang string) []llm.Message {
	systemContent := getExcerptSystemPrompt(lang, title)

	llmMsgs := make([]llm.Message, 0, 1+len(dbMessages))
	llmMsgs = append(llmMsgs, llm.Message{
		Role:    llm.RoleSystem,
		Content: systemContent,
	})

	for _, m := range dbMessages {
		role := llm.RoleUser
		if m.Role == 1 {
			role = llm.RoleAssistant
		}

		content := m.Content
		if role == llm.RoleAssistant {
			runes := []rune(content)
			if len(runes) > 1024 {
				content = string(runes[:500]) + "\n...\n" + string(runes[len(runes)-500:])
			}
		}

		// Only user messages get the [msg_id] numbering for excerpt reference.
		// Assistant messages are left unnumbered — they serve only as context.
		if role == llm.RoleUser {
			content = fmt.Sprintf("[%d] %s", m.ID, content)
		}

		llmMsgs = append(llmMsgs, llm.Message{
			Role:    role,
			Content: content,
		})
	}

	return llmMsgs
}

// ============================================================
// Standalone LLM call
// ============================================================

// CallExcerptLLMStandalone builds excerpt messages, sends to LLM via tool call,
// and returns parsed excerpt results. Usable from external packages like tasks.
func CallExcerptLLMStandalone(
	ctx context.Context,
	title string,
	dbMessages []store.Message,
	lang string,
	client llm.Client,
	apiKey string,
) *ExcerptResult {
	llmMsgs := buildExcerptLLMMessages(title, dbMessages, lang)
	if len(llmMsgs) <= 1 {
		return nil
	}

	return callExcerptLLMWithTool(ctx, llmMsgs, lang, client, apiKey)
}

// callExcerptLLMWithTool sends messages to the LLM with the trip_excerpts tool
// and parses the result.
func callExcerptLLMWithTool(
	ctx context.Context,
	llmMsgs []llm.Message,
	lang string,
	client llm.Client,
	apiKey string,
) *ExcerptResult {
	tripTool := toolimp.NewTripExcerptsTool(lang)

	reqBody := llm.ChatCompletionRequest{
		Model:    client.Model(),
		Messages: llmMsgs,
		Tools:    []llm.ToolDefinition{tripTool.GetDefinition()},
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	reqBody.ForceToolChoice(toolimp.TripExcerptsToolName)

	resp, err := client.ChatWithOptions(ctx, reqBody, apiKey)
	if err != nil {
		return nil
	}

	result := &ExcerptResult{}

	if len(resp.Choices) > 0 && resp.Choices[0].FinishReason == "tool_calls" {
		msg := resp.Choices[0].Message
		for _, tc := range msg.ToolCalls {
			if err := tripTool.SetArgument(tc.Function.Arguments); err != nil {
				continue
			}
			if _, err := tripTool.Execute(); err != nil {
				continue
			}
		}

		excerptsResult := tripTool.GetExcerptsResult()
		for _, item := range excerptsResult.Excerpts {
			result.Excerpts = append(result.Excerpts, ExcerptItem{
				ExcerptText:    item.ExcerptText,
				ValueTypes:     item.ValueTypes,
				ContextSummary: item.ContextSummary,
				Reason:         item.Reason,
				MsgID:          item.MsgID,
			})
		}
	}

	return result
}
