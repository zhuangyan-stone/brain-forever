package agent

import (
	"encoding/json"
	"net/http"

	"BrainForever/infra/i18n"
	"BrainForever/toolset"
)

// ============================================================
// Document title generation handler -POST /api/doc/title
// ============================================================

type portraitTitleRequest struct {
	Content string `json:"content"`
}

// OnGetPortraitTitle handles POST /api/user/portrait/title
// This endpoint is retained for backward compatibility (the frontend may still
// call it), but the primary title generation now happens server-side in the SSE
// handler and is sent as a "title" SSE event.
func (h *ChatAgent) OnGetPortraitTitle(w http.ResponseWriter, r *http.Request) {
	var req portraitTitleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		toolset.WriteError(w, "content is required", http.StatusBadRequest)
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

	title := generatePortraitTitle(r.Context(), client, lang, req.Content, llmApiSettings.ApiKey)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"title": title,
	})
}
