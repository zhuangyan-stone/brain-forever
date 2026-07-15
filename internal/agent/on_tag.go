package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/session"
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
		h.logger.Errorf("failed to select chat title tag groups. %v", err)
		toolset.WriteError(w, i18n.TL(h.defaultLang, "api_error_internal"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

// ============================================================
// Chat Tags handler - POST /api/chat/tags?sn=XXX
// ============================================================

// OnDeleteChatTag handles DELETE /api/chat/tags?chat=ID&tag=XXX
// Removes a specific tag from a chat session within a transaction.
// If the deleted tag was the last non-empty tag, sets taged=false.
// Returns the remaining non-empty tag count for frontend state sync.
func (h *ChatAgent) OnDeleteChatTag(w http.ResponseWriter, r *http.Request) {
	chatIDStr := r.URL.Query().Get("chat")
	tag := r.URL.Query().Get("tag")
	if chatIDStr == "" || tag == "" {
		toolset.WriteError(w, "missing chat or tag parameter", http.StatusBadRequest)
		return
	}

	var chatID int64
	if _, err := fmt.Sscan(chatIDStr, &chatID); err != nil {
		toolset.WriteError(w, "invalid chat id", http.StatusBadRequest)
		return
	}

	tagCount, err := theChatStore.DeleteChatTagAndUpdateTaged(chatID, tag)
	if err != nil {
		h.logger.Errorf("failed to delete chat tag. %v", err)
		toolset.WriteError(w, i18n.TL(h.defaultLang, "api_error_internal"), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"tagCount": tagCount,
	})
}

// OnGenerateChatTags handles POST /api/chat/tags?sn=XXX&force=true
// force=true bypasses the taged guard, allowing re-tagging even if already classified.
// Used by auto-tag when chat title changes.
func (h *ChatAgent) OnGenerateChatTags(w http.ResponseWriter, r *http.Request) {
	chatSN := r.URL.Query().Get("sn")
	if chatSN == "" {
		toolset.WriteError(w, i18n.TL(h.defaultLang, "api_error_sn_required"), http.StatusBadRequest)
		return
	}

	force := r.URL.Query().Get("force") == "true"

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	chatTitle, taged, chatID := searchChatBySN(sess, chatSN)

	if chatID == 0 {
		toolset.WriteError(w, i18n.TL(h.defaultLang, "api_error_chat_not_found"), http.StatusNotFound)
		return
	}

	if taged && !force {
		h.respondTaggedChat(w, chatID, chatSN, chatTitle)
		return
	}

	totalMessages := 0
	if count, err := theChatStore.CountMessages(chatID); err == nil {
		totalMessages = count
	}

	tags, viewedCount, allViewed, err := h.generateTagsViaLLM(r.Context(), sess, chatID, chatTitle, totalMessages, lang)
	if err != nil {
		h.logger.Errorf("%v", err)
		toolset.WriteError(w, i18n.TL(h.defaultLang, "api_error_llm_call_failed"), http.StatusInternalServerError)
		return
	}

	h.persistChatTags(chatID, chatSN, tags, sess)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sn":                chatSN,
		"title":             chatTitle,
		"tags":              tags,
		"totalMessages":     totalMessages,
		"viewedMessages":    viewedCount,
		"allMessagesViewed": allViewed,
	})
}

// persistChatTags persists tags to DB and updates the in-memory session cache.
// Uses ReplaceChatTags for atomic replace + taged update.
//
// taged is always set to true regardless of whether tags are empty:
//   - Non-empty tags: normal classification, tags are written to chat_tags.
//   - Empty tags: taged=true with no chat_tags records, meaning
//     "processed but no match", distinguished from "never classified" (taged=false).
func (h *ChatAgent) persistChatTags(chatID int64, chatSN string, tags []string, sess *session.Session) {
	if err := theChatStore.ReplaceChatTags(chatID, tags); err != nil {
		h.logger.Errorf("failed to replace chat tags for chat %d. %v", chatID, err)
	}

	// Update in-memory taged state
	sess.User.ChatsMu.Lock()
	for i := range sess.User.Chats {
		if sess.User.Chats[i].SN == chatSN {
			sess.User.Chats[i].Taged = true
			break
		}
	}
	sess.User.ChatsMu.Unlock()
}

// searchChatBySN looks up a chat by SN in the session's chat list.
// Returns the chat's title, taged status, and ID. If not found, returns 0 for chatID.
// The lock is acquired and released internally.
func searchChatBySN(sess *session.Session, chatSN string) (chatTitle string, taged bool, chatID int64) {
	sess.User.ChatsMu.Lock()
	defer sess.User.ChatsMu.Unlock()

	for _, c := range sess.User.Chats {
		if c.SN == chatSN {
			return c.Title, c.Taged, c.ID
		}
	}
	return "", false, 0
}

// respondTaggedChat reads existing tags from DB for an already-tagged chat
// and returns them as a JSON response with viewedMessages=0.
func (h *ChatAgent) respondTaggedChat(w http.ResponseWriter, chatID int64, chatSN string, chatTitle string) {
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
	json.NewEncoder(w).Encode(map[string]any{
		"sn":                chatSN,
		"title":             chatTitle,
		"tags":              tags,
		"totalMessages":     totalMessages,
		"viewedMessages":    0,
		"allMessagesViewed": false,
	})
}

// generateTagsViaLLM creates tools and runs the LLM tool-calling loop
// to generate chat tags. Returns the generated tags and viewing statistics.
func (h *ChatAgent) generateTagsViaLLM(
	ctx context.Context,
	sess *session.Session,
	chatID int64,
	chatTitle string,
	totalMessages int,
	lang string,
) (tags []string, viewedCount int, allViewed bool, err error) {
	tagUsageMap, _ := theChatStore.SelectNonEmptyTagsGroup()
	tagsUsageStr := formatTagsUsage(tagUsageMap)

	systemPrompt := i18n.SystemPrompt.TL(lang, "tag", map[string]any{
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

		resp, err := client.ChatWithOptions(ctx, req, llmApiSettings.ApiKey)
		if err != nil {
			return nil, 0, false, fmt.Errorf("chat tag LLM call failed. %w", err)
		}

		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
			break
		}

		toolCall := resp.Choices[0].Message.ToolCalls[0]

		switch toolCall.Function.Name {
		case toolimp.ChatSamplesToolName:
			sampleContent, err := samplesTool.Execute()
			if err != nil {
				h.logger.Errorf("chat samples tool execute failed. %v", err)
				sampleContent = i18n.Tools.TL(lang, "chat_samples_messages", "fetch_samples_failed", map[string]any{"Error": err.Error()})
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
				h.logger.Errorf("failed to parse chat tag arguments. %v", err)
			}
			gotit = true
		default:
			h.logger.Errorf("chat tag LLM called unknown tool: %s", toolCall.Function.Name)
		}
	}

	if tags == nil {
		tags = []string{}
	}

	return tags, samplesTool.GetViewedMessageCount(), samplesTool.IsAllMessagesViewed(), nil
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
