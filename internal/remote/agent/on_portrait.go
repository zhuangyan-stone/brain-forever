package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
)

// ============================================================
// Request / Response types
// ============================================================

// portraitTraitItem represents a single personal trait in the portrait request.
type portraitTraitItem struct {
	Text       string `json:"text"`
	Category   int    `json:"category"`
	Confidence int    `json:"confidence"`
	HalfLife   int    `json:"half_life"`
	CreateAt   string `json:"create_at"`
}

// portraitRequest is the JSON body for POST /api/portrait.
type portraitRequest struct {
	Lang    string              `json:"lang"`    // e.g. "zh-CN"
	Retouch int                 `json:"retouch"` // 0-5
	Traits  []portraitTraitItem `json:"traits"`  // user's personal traits
}

// ============================================================
// OnPortrait — POST /api/portrait handler (streaming)
//
// Request (JSON):
//
//	{
//	  "lang": "zh-CN",
//	  "retouch": 3,
//	  "traits": [
//	    {"text": "用户25岁", "category": 1, "confidence": 9, "half_life": 3, "create_at": "2026-06-20T10:00:00Z"},
//	    ...
//	  ]
//	}
//
// Response: SSE stream with text/event-stream content type.
// Each SSE data line is a JSON object:
//   - {"event":"text", "data":"..."}       — incremental text chunk
//   - {"event":"error", "data":"..."}      — error message
//   - {"event":"done", "data":{}}          — stream complete
//
// ============================================================
func OnPortrait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ----------------------------------------------------------
	// 1. Parse request body
	// ----------------------------------------------------------
	var req portraitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writePortraitJSONError(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Traits) == 0 {
		writePortraitJSONError(w, "no traits provided", http.StatusBadRequest)
		return
	}

	if req.Retouch < 0 || req.Retouch > 5 {
		req.Retouch = 3 // default
	}

	if req.Lang == "" {
		req.Lang = "zh-CN"
	}

	// ----------------------------------------------------------
	// 2. Build system prompt with i18n
	// ----------------------------------------------------------
	traitsJSON, err := json.Marshal(req.Traits)
	if err != nil {
		writePortraitJSONError(w, fmt.Sprintf("failed to marshal traits: %v", err), http.StatusInternalServerError)
		return
	}

	systemContent := i18n.SystemPrompt.TL(req.Lang, "portrait", map[string]interface{}{
		"Retouch":    req.Retouch,
		"TraitsJSON": string(traitsJSON),
	})

	// ----------------------------------------------------------
	// 3. Create DeepSeek client
	// ----------------------------------------------------------
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	baseURL := os.Getenv("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/"
	}
	model := os.Getenv("DEEPSEEK_MODEL")
	if model == "" {
		model = "deepseek-chat"
	}

	client := llm.NewDeepSeekClient(baseURL, apiKey, "DEEPSEEK_API_KEY", model)

	// ----------------------------------------------------------
	// 4. Build LLM messages and start streaming
	// ----------------------------------------------------------
	llmMsgs := []llm.Message{
		{Role: llm.RoleSystem, Content: systemContent},
		{Role: llm.RoleUser, Content: fmt.Sprintf("请基于以下 %d 条个人特征，生成我的用户个人画像。润色级别：%d。", len(req.Traits), req.Retouch)},
	}

	// Create a streaming request with thinking disabled (portrait generation
	// doesn't benefit from chain-of-thought reasoning).
	streamReq := llm.ChatCompletionRequest{
		Model:    model,
		Messages: llmMsgs,
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	streamReq.IncludeUsage(true)

	stream := client.ChatStreamWithOptions(r.Context(), streamReq)
	if err := stream.Err(); err != nil {
		writePortraitJSONError(w, fmt.Sprintf("LLM stream failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	// ----------------------------------------------------------
	// 5. Set up SSE writer and stream response
	// ----------------------------------------------------------
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writePortraitJSONError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sw := sse.NewSSEWriter(w)
	totalContent := ""

	for stream.Next() {
		chunk := stream.CurrentChatCompletionChunk()

		// Extract token usage from the final chunk
		if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
			// Store usage info but don't need to forward it for portrait
			client.SetUsageInfo(*chunk.Usage)
		}

		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			if content != "" {
				totalContent += content
				// Forward each text chunk as an SSE event
				sendPortraitSSE(sw, "text", content)
			}
		}

		// Check for context cancellation
		select {
		case <-r.Context().Done():
			sendPortraitSSE(sw, "error", "request cancelled")
			return
		default:
		}

		flusher.Flush()
	}

	if err := stream.Err(); err != nil {
		sendPortraitSSE(sw, "error", fmt.Sprintf("stream error: %v", err))
		return
	}

	// Send done signal
	sendPortraitSSE(sw, "done", map[string]interface{}{})
	flusher.Flush()
}

// ============================================================
// Helpers
// ============================================================

// ssePortraitEvent is the JSON structure for each SSE data message.
type ssePortraitEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// sendPortraitSSE marshals and writes a portrait SSE event.
func sendPortraitSSE(sw *sse.Writer, eventType string, data interface{}) {
	msg := ssePortraitEvent{Event: eventType, Data: data}
	b, _ := json.Marshal(msg)
	_ = sw.WriteRaw(string(b))
}

// writePortraitJSONError writes a JSON error response with the given status code.
func writePortraitJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
