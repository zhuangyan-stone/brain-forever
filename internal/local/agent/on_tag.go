package agent

import (
	"encoding/json"
	"net/http"
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
// It loads the chat's messages, sends them to the LLM along with the
// tag classification system prompt, and forces the LLM to call the
// "chat_tag" tool with the classification result.
//
// Query parameters:
//   - sn: the target chat SN (required)
//
// Returns a JSON object with the chat SN and an array of tag items:
//
//	{
//	  "sn": "chat-xxx",
//	  "tags": [
//	    {"category": "...", "tag": "..."},
//	    {"category": "...", "tag": "..."}
//	  ]
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

	// Load messages from DB
	dbMessages, err := session.chatsStore.ListMessages(dbSessionID)
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_failed_to_list_messages", map[string]interface{}{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	agentMsgs, convErr := convertDBMessagesToAgentMessages(dbMessages, session.chatsStore, dbSessionID)
	if convErr != nil {
		toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_failed_to_convert_messages", map[string]interface{}{"Error": convErr.Error()}), http.StatusInternalServerError)
		return
	}

	// Build the LLM prompt with the tag system prompt
	systemPrompt := i18n.SystemPrompt.TL(lang, "tag", nil)

	// Build conversation text from the messages
	var conversationBuilder strings.Builder
	conversationBuilder.WriteString(i18n.TL(lang, "chat_title_label"))
	conversationBuilder.WriteString(chatTitle)
	conversationBuilder.WriteString("\n\n")
	conversationBuilder.WriteString(i18n.TL(lang, "chat_conversation_label"))
	conversationBuilder.WriteString("\n")
	for _, msg := range agentMsgs {
		switch msg.Role {
		case llm.RoleUser:
			conversationBuilder.WriteString(i18n.TL(lang, "role_user_label"))
		case llm.RoleAssistant:
			conversationBuilder.WriteString(i18n.TL(lang, "role_assistant_label"))
		default:
			continue
		}
		conversationBuilder.WriteString(msg.Content)
		conversationBuilder.WriteString("\n")
	}

	llmMessages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: conversationBuilder.String()},
	}

	// Create the chat tag tool and force the LLM to use it
	tagTool := toolimp.MakeChatTagTool(lang)
	toolDefs := []llm.ToolDefinition{tagTool.GetDefinition()}

	req := llm.ChatCompletionRequest{
		Messages: llmMessages,
		Tools:    toolDefs,
	}
	req.ForceToolChoice(toolimp.ChatTagToolName)

	// Call LLM (non-streaming)
	resp, err := h.charLLMClient.ChatWithOptions(r.Context(), req)
	if err != nil {
		h.logger.Errorf("chat tag LLM call failed: %v", err)
		toolset.WriteJSONError(w, i18n.TL(h.defaultLang, "api_error_llm_call_failed"), http.StatusInternalServerError)
		return
	}

	// Extract tags from tool call response
	var tags []toolimp.TagItem
	if len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0 {
		toolCall := resp.Choices[0].Message.ToolCalls[0]
		if toolCall.Function.Name == toolimp.ChatTagToolName {
			if err := tagTool.SetArgument(toolCall.Function.Arguments); err == nil {
				tags = tagTool.Tags
			} else {
				h.logger.Errorf("failed to parse chat tag arguments: %v", err)
			}
		}
	}

	// Ensure we always return a valid JSON array
	if tags == nil {
		tags = []toolimp.TagItem{}
	}

	// Return the tags along with the chat SN
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sn":   chatSN,
		"tags": tags,
	})
}
