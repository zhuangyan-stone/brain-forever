package agent

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/remote/agent/toolimp"
	"BrainForever/toolset"
)

// ============================================================
// Request / Response types
// ============================================================

// traitsRequest is the JSON body for POST /api/traits.
type traitsRequest struct {
	SN       string      `json:"sn"`
	Title    string      `json:"title"`
	Messages []traitsMsg `json:"messages"`
}

// traitsMsg represents a single message in the request.
type traitsMsg struct {
	Role     string `json:"role"` // "user" or "assistant"
	Content  string `json:"content"`
	CreateAt string `json:"create_at"` // RFC3339 or "2006-01-02 15:04:05" format
}

// traitsResponse is the JSON response for POST /api/traits.
type traitsResponse struct {
	Features []toolimp.TripTraitsFeature `json:"features,omitempty"`
	Usage    *llm.Usage                  `json:"usage,omitempty"`
	Error    string                      `json:"error,omitempty"`
}

// ============================================================
// OnTripTraits — POST /api/traits handler
//
// Request (JSON):
//
//	{
//	  "sn": "chat-sn-xxx",
//	  "title": "chat title",
//	  "messages": [
//	    {"role": "user", "content": "...", "create_at": "2026-06-20 10:00:00"},
//	    {"role": "assistant", "content": "...", "create_at": "2026-06-20 10:01:00"}
//	  ]
//	}
//
// Response (JSON):
//
//	{
//	  "features": [...],
//	  "usage": {"prompt_tokens":..., "completion_tokens":..., "total_tokens":...}
//	}
//
// ============================================================
func OnTripTraits(w http.ResponseWriter, r *http.Request) {
	// Determine user language from request
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = "zh-CN"
	}

	// Only accept POST
	if r.Method != http.MethodPost {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	// ----------------------------------------------------------
	// 1. Parse request body
	// ----------------------------------------------------------
	var req traitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_invalid_json_body", map[string]interface{}{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.SN == "" {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_missing_sn_field"), http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_missing_messages_field"), http.StatusBadRequest)
		return
	}

	// ----------------------------------------------------------
	// 2. Build LLM messages from request data
	// ----------------------------------------------------------

	systemContent := getTraitSystemPrompt(lang, req.Title)

	llmMsgs := make([]llm.Message, 0, 1+len(req.Messages))
	llmMsgs = append(llmMsgs, llm.Message{
		Role:    llm.RoleSystem,
		Content: systemContent,
	})

	for _, m := range req.Messages {
		role := m.Role
		if role != llm.RoleUser && role != llm.RoleAssistant {
			continue
		}

		// Add timestamp prefix [YYYY-MM-DD HH:MM:SS] to help the analyzing LLM
		content := m.Content
		if m.CreateAt != "" {
			content = "[" + m.CreateAt + "] " + content
		}

		// For assistant messages: truncate to 1000 runes, skip reasoning
		if role == llm.RoleAssistant {
			runes := []rune(content)
			if len(runes) > 1024 {
				content = string(runes[:500]) + "\n...\n" + string(runes[len(runes)-500:])
			}
		}

		llmMsgs = append(llmMsgs, llm.Message{
			Role:    role,
			Content: content,
		})
	}

	if len(llmMsgs) <= 1 {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_no_valid_messages"), http.StatusBadRequest)
		return
	}

	// ----------------------------------------------------------
	// 3. Create DeepSeek client
	// ----------------------------------------------------------
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	baseURL := os.Getenv("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/beta"
	}
	model := os.Getenv("DEEPSEEK_MODEL")
	if model == "" {
		model = "deepseek-chat"
	}

	client := llm.NewDeepSeekClient(baseURL, apiKey, "DEEPSEEK_API_KEY", model)

	// ----------------------------------------------------------
	// 4. Create tool and request
	// ----------------------------------------------------------
	tripTool := toolimp.NewTripTraitsTool(lang)

	reqBody := llm.ChatCompletionRequest{
		Model:    model,
		Messages: llmMsgs,
		Tools:    []llm.ToolDefinition{tripTool.GetDefinition()},
	}

	// Force tool choice -only allow the LLM to call the trip_traits tool.
	reqBody.ForceToolChoice(toolimp.TripTraitsToolName)

	// Disable thinking to reduce latency and cost
	reqBody.Thinking = &llm.ThinkingConfig{Type: "disabled"}

	// ----------------------------------------------------------
	// 5. Call DeepSeek API (non-streaming)
	// ----------------------------------------------------------
	resp, err := client.ChatWithOptions(r.Context(), reqBody)
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_llm_call_failed", map[string]interface{}{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	// ----------------------------------------------------------
	// 6. Parse tool calls from the non-streaming response
	// ----------------------------------------------------------
	result := traitsResponse{}

	// Store usage info
	if resp.Usage != nil && resp.Usage.TotalTokens > 0 {
		result.Usage = resp.Usage
	}

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

		traitsResult := tripTool.GetTraitsResult()
		result.Features = traitsResult.Features
	} else if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
		// Fallback: try to parse JSON from the text response
		result.Error = "LLM returned text instead of tool call"
	}

	// ----------------------------------------------------------
	// 7. Write response
	// ----------------------------------------------------------
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ============================================================
// Helpers
// ============================================================

// getTraitSystemPrompt returns the localized system prompt for trip_traits extraction.
// The prompt content is stored in lang/remote/{lang}/system_prompt.toml under key "trip_trait".
// It injects the current local time and chat title into the {{.CurrentLocalTime}} and {{.ChatTitle}}
// template placeholders within the system prompt.
func getTraitSystemPrompt(lang string, chatTitle string) string {
	return i18n.SystemPrompt.TL(lang, "trip_trait", map[string]interface{}{
		"CurrentLocalTime": time.Now().In(time.Local).Format("2006-01-02 15:04:05 (MST)"),
		"ChatTitle":        chatTitle,
	})
}
