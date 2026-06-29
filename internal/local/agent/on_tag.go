package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/local/agent/toolimp"
	"BrainForever/toolset"
)

// ============================================================
// Chat Tags handler -GET /api/chat/tags?sn=XXX
// ============================================================

// OnMakeChatTags handles GET /api/chat/tags -classifies a chat conversation
// into topic categories/tags using the LLM with ToolCall.
//
// It loads the chat's title and selected user messages (with smart truncation),
// sends them to the LLM along with the tag classification system prompt,
// and forces the LLM to call the "chat_tag" tool with the classification result.
//
// The system prompt is dynamically populated with the user's existing tags
// (from SelectTagsGroup) via the {{.TagsUsage}} template variable.
//
// Query parameters:
//   - sn: the target chat SN (required)
//
// Returns a JSON object with the chat SN and an array of tag strings:
//
//	{
//	  "sn": "chat-xxx",
//	  "tags": ["技术", "生活"]
//	}
func (h *ChatAgent) OnMakeChatTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the required sn parameter
	chatSN := r.URL.Query().Get("sn")
	if chatSN == "" {
		toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_sn_required"), http.StatusBadRequest)
		return
	}

	// Determine the language for this request
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// Resolve sessionID from cookie
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Look up the chat by SN from the session's chat list
	var dbSessionID int64
	var chatTitle string
	session.chatsMu.Lock()
	for _, c := range session.chats {
		if c.SN == chatSN {
			dbSessionID = c.ID
			chatTitle = c.Title
			break
		}
	}
	session.chatsMu.Unlock()

	if dbSessionID == 0 {
		toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_chat_not_found"), http.StatusNotFound)
		return
	}

	// Build the LLM prompt with the tag system prompt.
	// 1. Load user's existing tags with usage counts
	tagUsageMap, _ := session.chatsStore.SelectTagsGroup()
	tagsUsageStr := formatTagsUsage(tagUsageMap)

	// 2. Build system prompt with TagsUsage template data
	systemPrompt := i18n.SystemPrompt.TL(lang, "tag", map[string]interface{}{
		"TagsUsage": tagsUsageStr,
	})

	// 3. The user content is just the chat title.
	// The LLM can call get_chat_samples_messages if it needs more context.
	userContent := chatTitle

	// 4. Create tools
	tagTool := toolimp.MakeChatTagTool(lang)
	samplesTool := toolimp.MakeChatSamplesTool(lang, session.chatsStore, chatSN, chatTitle)
	toolDefs := []llm.ToolDefinition{
		samplesTool.GetDefinition(),
		tagTool.GetDefinition(),
	}

	// 5. Multi-turn tool call loop
	llmMessages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: userContent},
	}

	maxIter := 3
	var tags []string

	for iter := 0; iter < maxIter; iter++ {
		req := llm.ChatCompletionRequest{
			Messages: llmMessages,
			Tools:    toolDefs,
		}
		// Disable thinking mode since we force the LLM to only call tools,
		// without generating any text content. Enabling thinking would waste tokens.
		req.Thinking = &llm.ThinkingConfig{Type: "disabled"}

		if iter == 0 {
			// First iteration: let LLM choose between get_chat_samples_messages or chat_tag
			req.RequiredToolChoice()
		} else {
			// Subsequent iterations: force chat_tag
			toolDefs = []llm.ToolDefinition{tagTool.GetDefinition()}
			req.Tools = toolDefs
			req.ForceToolChoice(toolimp.ChatTagToolName)
		}

		// Call LLM (non-streaming)
		resp, err := h.charLLMClient.ChatWithOptions(r.Context(), req)
		if err != nil {
			h.logger.Errorf("chat tag LLM call failed: %v", err)
			toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_llm_call_failed"), http.StatusInternalServerError)
			return
		}

		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
			h.logger.Errorf("chat tag LLM returned no tool calls")
			// Break with empty tags
			break
		}

		toolCall := resp.Choices[0].Message.ToolCalls[0]

		switch toolCall.Function.Name {
		case toolimp.ChatSamplesToolName:
			// The LLM wants more context. Use the incremental loader to fetch
			// the next batch of messages from DB (up to pageSize=10 per call).
			sampleContent, err := samplesTool.Execute()
			if err != nil {
				h.logger.Errorf("chat samples tool execute failed: %v", err)
				sampleContent = fmt.Sprintf("获取消息样本失败: %v", err)
			}

			// Build assistant message with tool call
			assistantMsg := llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{{
					ID:   toolCall.ID,
					Type: toolCall.Type,
					Function: llm.ToolCallFunction{
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					},
				}},
			}

			// Build tool result message
			toolResultMsg := llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCall.ID,
				Content:    sampleContent,
			}

			llmMessages = append(llmMessages, assistantMsg, toolResultMsg)
			// Continue loop to force chat_tag

		case toolimp.ChatTagToolName:
			// Parse the classification result
			if err := tagTool.SetArgument(toolCall.Function.Arguments); err == nil {
				tags = tagTool.Tags
			} else {
				h.logger.Errorf("failed to parse chat tag arguments: %v", err)
			}
			// We have the result, break out of the loop
			iter = maxIter // break

		default:
			h.logger.Errorf("chat tag LLM called unknown tool: %s", toolCall.Function.Name)
		}
	}

	// Ensure we always return a valid JSON array
	if tags == nil {
		tags = []string{}
	}

	// Return the tags along with the chat SN and title
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sn":    chatSN,
		"title": chatTitle,
		"tags":  tags,
	})
}

// formatTagsUsage formats the tag usage map into a human-readable string
// sorted by count descending. Returns empty string if the map is empty.
//
// Example output:
//
//   - 技术 5次
//   - 生活 3次
//   - 娱乐 2次
func formatTagsUsage(tagUsageMap map[string]int) string {
	if len(tagUsageMap) == 0 {
		return "（暂无）"
	}

	type tagCount struct {
		Tag   string
		Count int
	}

	var sorted []tagCount
	for t, c := range tagUsageMap {
		sorted = append(sorted, tagCount{t, c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})

	var b strings.Builder
	for _, tc := range sorted {
		b.WriteString(fmt.Sprintf("- %s %d次\n", tc.Tag, tc.Count))
	}
	return b.String()
}
