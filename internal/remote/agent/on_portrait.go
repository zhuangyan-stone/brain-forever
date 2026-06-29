package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/remote/agent/toolimp"
	"BrainForever/toolset"
)

// confidenceLevel returns a human-readable confidence level description key suffix
// for the given confidence value (1-10).
func confidenceLevelKey(confidence int) string {
	switch {
	case confidence >= 8:
		return "high"
	case confidence >= 4:
		return "medium"
	default:
		return "low"
	}
}

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

// ToString returns a human-readable natural language representation of this trait item,
// with explanations for each field (category, confidence, half-life, creation time).
// The lang parameter controls the language of field labels and descriptions.
func (t portraitTraitItem) ToString(lang string, index int) string {
	// Category name and description
	catName := i18n.TL(lang, fmt.Sprintf("trait_cat_%d", t.Category))
	catDesc := i18n.TL(lang, fmt.Sprintf("trait_cat_desc_%d", t.Category))

	// Half-life name and description
	hlName := i18n.TL(lang, fmt.Sprintf("trait_halflife_%d", t.HalfLife))
	hlDesc := i18n.TL(lang, fmt.Sprintf("trait_halflife_desc_%d", t.HalfLife))

	// Confidence level description
	confLevel := i18n.TL(lang, "trait_confidence_"+confidenceLevelKey(t.Confidence))

	return i18n.TL(lang, "trait_item_format", map[string]interface{}{
		"Index":         index,
		"Text":          t.Text,
		"CatID":         t.Category,
		"CatName":       catName,
		"CatDesc":       catDesc,
		"ConfValue":     t.Confidence,
		"ConfLevel":     confLevel,
		"HalfLifeName":  hlName,
		"HalfLifeValue": t.HalfLife,
		"HalfLifeDesc":  hlDesc,
		"CreateAt":      t.CreateAt,
	})
}

// formatTraitItems converts a slice of portraitTraitItem into a natural language
// string that explains each trait with field descriptions, separated by blank lines.
func formatTraitItems(items []portraitTraitItem, lang string) string {
	var sb strings.Builder
	for i, item := range items {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(item.ToString(lang, i+1))
	}
	return sb.String()
}

// portraitRequest is the JSON body for POST /api/portrait.
type portraitRequest struct {
	Lang     string              `json:"lang"`      // e.g. "zh-CN"
	Retouch  int                 `json:"retouch"`   // 0-5
	Traits   []portraitTraitItem `json:"traits"`    // user's personal traits
	TagsInfo string              `json:"tags_info"` // "你的话题最热门领域是：技术(5次)、生活(3次)..."
}

// ============================================================
// OnTripPortrait — POST /api/portrait handler (streaming)
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
func OnTripPortrait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Determine user language from request
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))

	// ----------------------------------------------------------
	// 1. Parse request body
	// ----------------------------------------------------------
	var req portraitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_invalid_json_body", map[string]interface{}{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if len(req.Traits) == 0 {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_no_traits_provided"), http.StatusBadRequest)
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
	traitsDesc := formatTraitItems(req.Traits, req.Lang)

	systemContent := i18n.SystemPrompt.TL(req.Lang, "portrait", map[string]interface{}{
		"Retouch":    req.Retouch,
		"TraitsJSON": traitsDesc,
		"TagsInfo":   req.TagsInfo,
	})

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
	// 4. Build LLM messages and start streaming
	// ----------------------------------------------------------
	userContent := i18n.SystemPrompt.TL(lang, "portrait_user_prompt", map[string]interface{}{
		"TraitCount": len(req.Traits),
		"Retouch":    req.Retouch,
	})
	llmMsgs := []llm.Message{
		{Role: llm.RoleSystem, Content: systemContent},
		{Role: llm.RoleUser, Content: userContent},
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
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_llm_stream_failed", map[string]interface{}{"Error": err.Error()}), http.StatusInternalServerError)
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
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_streaming_not_supported"), http.StatusInternalServerError)
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

	// ----------------------------------------------------------
	// 6. Step 2: Extract structured metadata (core traits + key highlights)
	//    from the completed portrait text.
	// ----------------------------------------------------------
	if totalContent != "" {
		meta := extractPortraitHighlights(r.Context(), client, req.Lang, totalContent)
		if meta != nil {
			sendPortraitSSE(sw, "meta", meta)
			flusher.Flush()
		}
	}

	// ----------------------------------------------------------
	// 7. Send done signal
	// ----------------------------------------------------------
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
// Uses json.Encoder with SetEscapeHTML(false) to prevent Go's default JSON
// encoder from escaping '>', '<', '&' to \u003e, \u003c, \u0026.
// SSE data is not embedded in HTML, so HTML escaping is unnecessary.
func sendPortraitSSE(sw *sse.Writer, eventType string, data interface{}) {
	msg := ssePortraitEvent{Event: eventType, Data: data}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(msg); err != nil {
		return
	}
	// json.Encoder.Encode appends a trailing '\n'; strip it for consistency
	// with the previous json.Marshal behavior (no trailing newline).
	raw := buf.Bytes()
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	_ = sw.WriteRaw(string(raw))
}

// PortraitHighlights holds the structured metadata extracted from a user portrait.
type PortraitHighlights struct {
	CoreTraits    []string `json:"core_traits"`
	KeyHighlights []string `json:"key_highlights"`
}

// extractPortraitHighlights makes a non-streaming LLM call with the completed portrait
// text as input, using ForceToolChoice to invoke the trip_highlights tool.
// Returns nil if extraction fails (the error is non-fatal — the portrait text
// has already been streamed to the frontend).
func extractPortraitHighlights(ctx context.Context, client llm.Client, lang, portraitText string) *PortraitHighlights {
	// 1. Build system prompt from i18n template (single message, no user message needed)
	systemContent := i18n.SystemPrompt.TL(lang, "highlights", map[string]interface{}{
		"PortraitText": portraitText,
	})

	// 2. Create the trip_highlights tool
	extractTool := toolimp.NewTripHighlightsTool(lang)

	// 3. Build request with ForceToolChoice (single system message)
	req := llm.ChatCompletionRequest{
		Model: client.Model(),
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemContent},
		},
		Tools:    []llm.ToolDefinition{extractTool.GetDefinition()},
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	req.ForceToolChoice(toolimp.TripHighlightsToolName)

	// 4. Call LLM (non-streaming)
	resp, err := client.ChatWithOptions(ctx, req)
	if err != nil {
		// Non-fatal: portrait text has already been sent
		return nil
	}

	// 5. Parse tool call from response
	if len(resp.Choices) == 0 {
		return nil
	}

	msg := resp.Choices[0].Message
	for _, tc := range msg.ToolCalls {
		if tc.Function.Name == toolimp.TripHighlightsToolName {
			if err := extractTool.SetArgument(tc.Function.Arguments); err != nil {
				continue
			}
			result := extractTool.GetResult()
			return &PortraitHighlights{
				CoreTraits:    result.CoreTraits,
				KeyHighlights: result.KeyHighlights,
			}
		}
	}

	return nil
}
