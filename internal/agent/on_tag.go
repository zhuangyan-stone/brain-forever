package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/toolset"
)

// ============================================================
// Chat groups handler -GET /api/chat/groups
// ============================================================

// OnChatGroups handles GET /api/chat/groups
func (h *ChatAgent) OnChatGroups(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	groups, err := theChatStore.SelectChatTitlesGroupByTags(sess.User.ID)
	if err != nil {
		h.logger.Errorf("failed to select chat title tag groups: %v", err)
		toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_internal"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

// ============================================================
// Chat Tags handler -GET /api/chat/tags?sn=XXX
// ============================================================

// OnGenerateChatTags handles GET /api/chat/tags
func (h *ChatAgent) OnGenerateChatTags(w http.ResponseWriter, r *http.Request) {
	chatSN := r.URL.Query().Get("sn")
	if chatSN == "" {
		toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_sn_required"), http.StatusBadRequest)
		return
	}

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	var chatTitle string
	var taged bool
	var chatID int64
	sess.User.ChatsMu.Lock()
	for _, c := range sess.User.Chats {
		if c.SN == chatSN {
			chatTitle = c.Title
			taged = c.Taged
			chatID = c.ID
			break
		}
	}
	sess.User.ChatsMu.Unlock()

	if chatID == 0 {
		toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_chat_not_found"), http.StatusNotFound)
		return
	}

	if taged {
		existingTags, listErr := theChatStore.ListChatTagsByChatID(chatID)
		var tags []string
		if listErr == nil {
			for _, ct := range existingTags {
				if ct.Tag != "" {
					tags = append(tags, ct.Tag)
				}
			}
		}
		if tags == nil {
			tags = []string{}
		}

		totalMessages := 0
		if count, err := theChatStore.CountMessages(chatID); err == nil {
			totalMessages = count
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sn":                chatSN,
			"title":             chatTitle,
			"tags":              tags,
			"totalMessages":     totalMessages,
			"viewedMessages":    0,
			"allMessagesViewed": false,
		})
		return
	}

	totalMessages := 0
	if count, err := theChatStore.CountMessages(chatID); err == nil {
		totalMessages = count
	}

	tagUsageMap, _ := theChatStore.SelectNonEmptyTagsGroup()
	tagsUsageStr := formatTagsUsage(tagUsageMap)

	systemPrompt := i18n.SystemPrompt.TL(lang, "tag", map[string]interface{}{
		"TagsUsage": tagsUsageStr,
		"Title":     chatTitle,
	})

	tagTool := toolimp.MakeChatTagTool(lang)
	samplesTool := toolimp.MakeChatSamplesTool(lang, theChatStore, chatID, chatTitle, totalMessages)
	toolDefs := []llm.ToolDefinition{
		samplesTool.GetDefinition(),
		tagTool.GetDefinition(),
	}

	llmMessages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
	}

	maxIter := 50
	var tags []string

	gotit := false
	for iter := 0; !gotit && iter < maxIter; iter++ {
		req := llm.ChatCompletionRequest{
			Messages: llmMessages,
			Tools:    toolDefs,
		}
		req.Thinking = &llm.ThinkingConfig{Type: "disabled"}
		req.EnableToolChoice()

		client := sessionLLMClient(sess)
		llmApiSettings := sessionLLMApiSetting(sess)

		resp, err := client.ChatWithOptions(r.Context(), req, llmApiSettings.ApiKey)
		if err != nil {
			h.logger.Errorf("chat tag LLM call failed: %v", err)
			toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_llm_call_failed"), http.StatusInternalServerError)
			return
		}

		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
			h.logger.Errorf("chat tag LLM returned no tool calls")
			break
		}

		toolCall := resp.Choices[0].Message.ToolCalls[0]

		switch toolCall.Function.Name {
		case toolimp.ChatSamplesToolName:
			sampleContent, err := samplesTool.Execute()
			if err != nil {
				h.logger.Errorf("chat samples tool execute failed: %v", err)
				sampleContent = i18n.Tools.TL(lang, "chat_samples_messages", "fetch_samples_failed", map[string]interface{}{"Error": err.Error()})
			}

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

			toolResultMsg := llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCall.ID,
				Content:    sampleContent,
			}

			llmMessages = append(llmMessages, assistantMsg, toolResultMsg)

		case toolimp.ChatTagToolName:
			if err := tagTool.SetArgument(toolCall.Function.Arguments); err == nil {
				tags = tagTool.Tags
			} else {
				h.logger.Errorf("failed to parse chat tag arguments: %v", err)
			}
			gotit = true
		default:
			h.logger.Errorf("chat tag LLM called unknown tool: %s", toolCall.Function.Name)
		}
	}

	if tags == nil {
		tags = []string{}
	}

	if delErr := theChatStore.DeleteChatTagsByChatID(chatID); delErr != nil {
		h.logger.Errorf("failed to delete old chat tags for chat %d: %v", chatID, delErr)
	}

	if len(tags) == 0 {
		if _, insErr := theChatStore.InsertChatTag(chatID, ""); insErr != nil {
			h.logger.Errorf("failed to insert empty chat tag for chat %d: %v", chatID, insErr)
		}
	} else {
		for _, tag := range tags {
			if _, insErr := theChatStore.InsertChatTag(chatID, tag); insErr != nil {
				h.logger.Errorf("failed to insert chat tag %q for chat %d: %v", tag, chatID, insErr)
			}
		}
	}

	if chatID > 0 {
		if tagErr := theChatStore.UpdateChatTagged(chatID, true); tagErr != nil {
			h.logger.Errorf("failed to update chat taged flag for chat %s: %v", chatSN, tagErr)
		}
	}

	sess.User.ChatsMu.Lock()
	for i := range sess.User.Chats {
		if sess.User.Chats[i].SN == chatSN {
			sess.User.Chats[i].Taged = true
			break
		}
	}
	sess.User.ChatsMu.Unlock()

	viewedCount := samplesTool.GetViewedMessageCount()
	allViewed := samplesTool.IsAllMessagesViewed()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sn":                chatSN,
		"title":             chatTitle,
		"tags":              tags,
		"totalMessages":     totalMessages,
		"viewedMessages":    viewedCount,
		"allMessagesViewed": allViewed,
	})
}

func formatTagsUsage(tagUsageMap map[string]int) string {
	if len(tagUsageMap) == 0 {
		return "(none)"
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
		b.WriteString(fmt.Sprintf("- %s %d times\n", tc.Tag, tc.Count))
	}
	return b.String()
}
