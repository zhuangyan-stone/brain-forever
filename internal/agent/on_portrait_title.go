package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/toolset"
)

// ============================================================
// Document title generation handler -POST /api/doc/title
// ============================================================

type portraitTitleRequest struct {
	Content string `json:"content"`
}

// OnGetPortraitTitle handles POST /api/user/portrait/title
func (h *ChatAgent) OnGetPortraitTitle(w http.ResponseWriter, r *http.Request) {
	var req portraitTitleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteJSONError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		toolset.WriteJSONError(w, "content is required", http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	client := sessionLLMClient(sess)
	llmApiSettings := sessionLLMApiSetting(sess)

	systemContent := i18n.SystemPrompt.TL(lang, "doc_title", nil)
	userContent := req.Content

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemContent},
		{Role: llm.RoleUser, Content: userContent},
	}

	titleCtx, titleCancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer titleCancel()

	resp, err := client.Chat(titleCtx, messages, llmApiSettings.ApiKey)
	if err != nil {
		toolset.WriteJSONError(w, "failed to generate title: "+err.Error(), http.StatusInternalServerError)
		return
	}

	title := ""
	if len(resp.Choices) > 0 {
		title = resp.Choices[0].Message.Content
	}

	const maxTitleLen = 50.0
	if title == "" || toolset.VisualLength(title) > maxTitleLen {
		title = ""
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"title": title,
	})
}
