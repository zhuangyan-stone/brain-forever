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
// Document title generation handler — POST /api/doc/title
//
// Request (JSON):
//
//	{
//	  "content": "（用户画像完整正文）"
//	}
//
// Response (JSON):
//
//	{
//	  "title": "生成的标题"
//	}
//
// Flow:
//  1. Frontend sends the completed portrait text
//  2. Local-server uses LLM with [doc_title] prompt to generate a concise title
//  3. Returns the title as JSON
// ============================================================

// docTitleRequest is the JSON body for POST /api/doc/title.
type docTitleRequest struct {
	Content string `json:"content"`
}

// OnGetDocTitle handles POST /api/doc/title — generates a concise
// overall title for a document (e.g., user portrait) using the local LLM.
func (h *ChatAgent) OnGetDocTitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ----------------------------------------------------------
	// 1. Parse request body
	// ----------------------------------------------------------
	var req docTitleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteJSONError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		toolset.WriteJSONError(w, "content is required", http.StatusBadRequest)
		return
	}

	// ----------------------------------------------------------
	// 2. Determine language
	// ----------------------------------------------------------
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = h.defaultLang
	}

	// ----------------------------------------------------------
	// 3. Build the LLM prompt
	// ----------------------------------------------------------
	systemContent := i18n.SystemPrompt.TL(lang, "doc_title", nil)

	userContent := req.Content

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemContent},
		{Role: llm.RoleUser, Content: userContent},
	}

	// ----------------------------------------------------------
	// 4. Call LLM (non-streaming) with a 30-second timeout
	// ----------------------------------------------------------
	titleCtx, titleCancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer titleCancel()

	resp, err := h.charLLMClient.Chat(titleCtx, messages)
	if err != nil {
		toolset.WriteJSONError(w, "failed to generate title: "+err.Error(), http.StatusInternalServerError)
		return
	}

	title := ""
	if len(resp.Choices) > 0 {
		title = resp.Choices[0].Message.Content
	}

	// Validate: if empty or too long (>50 visual chars), return empty
	const maxTitleLen = 50.0
	if title == "" || toolset.VisualLength(title) > maxTitleLen {
		title = ""
	}

	// ----------------------------------------------------------
	// 5. Return the title as JSON
	// ----------------------------------------------------------
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"title": title,
	})
}
