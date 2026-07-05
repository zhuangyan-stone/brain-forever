package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
	"BrainForever/toolset"
)

// ============================================================
// Portrait generation handler — GET /api/user/portrait?retouch=N
//
// Flow:
//  1. Frontend sends GET /api/user/portrait?retouch=3
//  2. Local-server reads ALL personal traits from user-specific traits DB
//  3. Local-server calls DeepSeek API directly (streaming) to generate portrait
//  4. Local-server streams LLM portrait text via SSE to frontend
//  5. After streaming completes, makes a second LLM call to extract highlights
//
// Traits DB naming (same as trait extraction):
//   - Anonymous: localdb/anonymous.brain.db
//   - Logged-in: localdb/{userNo}.brain.db
// ============================================================

// portraitTraitItem represents a single trait sent to the LLM.
type portraitTraitItem struct {
	Text       string `json:"text"`
	Category   int    `json:"category"`
	Confidence int    `json:"confidence"`
	HalfLife   int    `json:"half_life"`
	CreateAt   string `json:"create_at"`
}

// ToString returns a human-readable natural language representation of this trait item,
// with explanations for each field (category, confidence, half-life, creation time).
func (t portraitTraitItem) ToString(lang string, index int) string {
	catName := i18n.TL(lang, fmt.Sprintf("trait_cat_%d", t.Category))
	catDesc := i18n.TL(lang, fmt.Sprintf("trait_cat_desc_%d", t.Category))

	hlName := i18n.TL(lang, fmt.Sprintf("trait_halflife_%d", t.HalfLife))
	hlDesc := i18n.TL(lang, fmt.Sprintf("trait_halflife_desc_%d", t.HalfLife))

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

// confidenceLevelKey returns a confidence level description key suffix for the given value (1-10).
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

// formatTraitItems converts a slice of portraitTraitItem into a natural language string.
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

// portraitChatTitleItem represents a single chat title sent to the LLM.
type portraitChatTitleItem struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	CrateAt string `json:"create_at"`
}

// formatChatTitles converts a slice of chatTitleItem into a natural language string.
func formatChatTitles(items []portraitChatTitleItem, lang string) string {
	if len(items) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(i18n.TL(lang, "chat_titles_header"))
	sb.WriteString("\n")
	for i, item := range items {
		if item.Title == "" {
			continue
		}
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(i18n.TL(lang, "chat_title_item_format", map[string]interface{}{
			"Index":    i + 1,
			"Title":    item.Title,
			"CreateAt": item.CrateAt,
		}))
	}
	return sb.String()
}

// hotTagItem represents a single tag with its conversation count.
type hotTagItem struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ============================================================
// Portrait SSE types (same format as before)
// ============================================================

// ssePortraitEvent is the JSON structure for each SSE data message.
type ssePortraitEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// sendPortraitSSE marshals and writes a portrait SSE event.
func sendPortraitSSE(sw *sse.Writer, eventType string, data interface{}) {
	msg := ssePortraitEvent{Event: eventType, Data: data}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(msg); err != nil {
		return
	}
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

// ============================================================
// OnGetUserPortrait — GET /api/user/portrait handler
// ============================================================

func (h *ChatAgent) OnGetUserPortrait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ----------------------------------------------------------
	// 1. Parse query parameters
	// ----------------------------------------------------------
	retouchStr := r.URL.Query().Get("retouch")
	retouch := 3 // default
	if retouchStr != "" {
		if v, err := strconv.Atoi(retouchStr); err == nil && v >= 0 && v <= 5 {
			retouch = v
		}
	}

	// ----------------------------------------------------------
	// 2. Resolve session and get user traits
	// ----------------------------------------------------------
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))

	vs, err := session.ensureTraitsStore()
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_traits_store_unavailable"), http.StatusInternalServerError)
		return
	}

	allTraits, err := vs.ListAllTraitsByCreateTime()
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_failed_to_read_traits", map[string]interface{}{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	if len(allTraits) == 0 {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_no_traits_data"), http.StatusNotFound)
		return
	}

	// ----------------------------------------------------------
	// 3. Read tags and chat titles
	// ----------------------------------------------------------
	tagUsageMap, _ := session.chatsStore.SelectNonEmptyTagsGroup()
	hotTags := formatHotTags(tagUsageMap)
	tagsInfoStr := buildTagsInfoString(hotTags, lang)

	// Convert traits to portrait request format
	traitItems := make([]portraitTraitItem, 0, len(allTraits))
	for _, t := range allTraits {
		traitItems = append(traitItems, portraitTraitItem{
			Text:       t.Trait,
			Category:   t.Category,
			Confidence: t.Confidence,
			HalfLife:   t.HalfLife,
			CreateAt:   t.CreateAt.Local().Format(time.RFC3339),
		})
	}

	recentChatTitles, err := session.chatsStore.ListChatTitles(100)
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_failed_to_list_recent_chat_titles",
			map[string]interface{}{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	chatTitleItems := make([]portraitChatTitleItem, 0, len(recentChatTitles))
	for _, t := range recentChatTitles {
		chatTitleItems = append(chatTitleItems, portraitChatTitleItem{
			ID:      t.ID,
			Title:   t.Title,
			CrateAt: t.CrateAt.Local().Format(time.RFC3339),
		})
	}

	// ----------------------------------------------------------
	// 4. Build LLM system prompt and user prompt
	// ----------------------------------------------------------
	traitsDesc := formatTraitItems(traitItems, lang)
	chatTitlesStr := formatChatTitles(chatTitleItems, lang)

	systemContent := i18n.SystemPrompt.TL(lang, "portrait", map[string]interface{}{
		"Retouch":          retouch,
		"TraitsJSON":       traitsDesc,
		"TagsInfo":         tagsInfoStr,
		"RecentChatTitles": chatTitlesStr,
	})

	userContent := i18n.SystemPrompt.TL(lang, "portrait_user_prompt", map[string]interface{}{
		"TraitCount": len(allTraits),
		"Retouch":    retouch,
	})

	llmMsgs := []llm.Message{
		{Role: llm.RoleSystem, Content: systemContent},
		{Role: llm.RoleUser, Content: userContent},
	}

	// ----------------------------------------------------------
	// 5. Set SSE headers for frontend
	// ----------------------------------------------------------
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	sw := sse.NewSSEWriter(w)

	// ----------------------------------------------------------
	// 6. Compute portrait info and send as 'info' SSE event
	// ----------------------------------------------------------
	info := computePortraitInfo(allTraits, retouch, hotTags)
	if infoJSON, err := json.Marshal(info); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", infoJSON)
		flusher.Flush()
	}

	// ----------------------------------------------------------
	// 7. Call LLM directly (streaming) and forward to frontend
	// ----------------------------------------------------------
	acceptLang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))

	streamReq := llm.ChatCompletionRequest{
		Model:    h.charLLMClient.Model(),
		Messages: llmMsgs,
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	streamReq.IncludeUsage(true)

	stream := h.charLLMClient.ChatStreamWithOptions(r.Context(), streamReq)
	if err := stream.Err(); err != nil {
		sendPortraitSSE(sw, "error", fmt.Sprintf("LLM stream failed: %v", err))
		flusher.Flush()
		io.Copy(io.Discard, r.Body)
		return
	}
	defer stream.Close()

	totalContent := ""
	for stream.Next() {
		chunk := stream.CurrentChatCompletionChunk()

		if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
			h.charLLMClient.SetUsageInfo(*chunk.Usage)
		}

		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			if content != "" {
				totalContent += content
				sendPortraitSSE(sw, "text", content)
			}
		}

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
		flusher.Flush()
		return
	}

	// ----------------------------------------------------------
	// 8. Extract structured metadata (core traits + key highlights)
	// ----------------------------------------------------------
	if totalContent != "" {
		meta := extractPortraitHighlights(r.Context(), h.charLLMClient, acceptLang, totalContent)
		if meta != nil {
			sendPortraitSSE(sw, "meta", meta)
			flusher.Flush()
		}
	}

	// ----------------------------------------------------------
	// 9. Send done signal
	// ----------------------------------------------------------
	sendPortraitSSE(sw, "done", map[string]interface{}{})
	flusher.Flush()

	// Drain any remaining body
	io.Copy(io.Discard, r.Body)
}

// ============================================================
// Highlights extraction (second LLM call)
// ============================================================

// extractPortraitHighlights makes a non-streaming LLM call with the completed portrait
// text as input, using ForceToolChoice to invoke the trip_highlights tool.
func extractPortraitHighlights(ctx context.Context, client llm.Client, lang, portraitText string) *PortraitHighlights {
	systemContent := i18n.SystemPrompt.TL(lang, "highlights", map[string]interface{}{
		"PortraitText": portraitText,
	})

	extractTool := toolimp.NewTripHighlightsTool(lang)

	req := llm.ChatCompletionRequest{
		Model: client.Model(),
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemContent},
		},
		Tools:    []llm.ToolDefinition{extractTool.GetDefinition()},
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	req.ForceToolChoice(toolimp.TripHighlightsToolName)

	resp, err := client.ChatWithOptions(ctx, req)
	if err != nil {
		return nil
	}

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

// ============================================================
// portraitInfo — extra metadata sent as 'info' SSE event
// ============================================================

type portraitInfo struct {
	Event string           `json:"event"`
	Data  portraitInfoData `json:"data"`
}

type portraitInfoData struct {
	GeneratedAt  string       `json:"generated_at"`
	ChatCount    int          `json:"chat_count"`
	TraitCount   int          `json:"trait_count"`
	SpanDays     int          `json:"span_days"`
	EarliestDate string       `json:"earliest_date"`
	LatestDate   string       `json:"latest_date"`
	Retouch      int          `json:"retouch"`
	HotTags      []hotTagItem `json:"hot_tags"`
}

func computePortraitInfo(allTraits []store.PersonalTrait, retouch int, hotTags []hotTagItem) portraitInfo {
	chatSNSet := make(map[string]struct{})
	for _, t := range allTraits {
		if t.ChatSN != "" {
			chatSNSet[t.ChatSN] = struct{}{}
		}
	}
	chatCount := len(chatSNSet)

	earliestStr := ""
	latestStr := ""
	spanDays := 0

	n := len(allTraits)
	if n > 0 {
		latest := allTraits[0].CreateAt
		earliest := allTraits[n-1].CreateAt
		earliestStr = earliest.Format("2006-01-02")
		latestStr = latest.Format("2006-01-02")

		latestDate := time.Date(latest.Year(), latest.Month(), latest.Day(), 0, 0, 0, 0, latest.Location())
		earliestDate := time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, earliest.Location())
		spanDays = int(latestDate.Sub(earliestDate).Hours()/24) + 1
	}

	return portraitInfo{
		Event: "info",
		Data: portraitInfoData{
			GeneratedAt:  time.Now().Format("2006-01-02 15:04:05"),
			ChatCount:    chatCount,
			TraitCount:   n,
			SpanDays:     spanDays,
			EarliestDate: earliestStr,
			LatestDate:   latestStr,
			Retouch:      retouch,
			HotTags:      hotTags,
		},
	}
}

// ============================================================
// Tags helpers
// ============================================================

func formatHotTags(tagUsageMap map[string]int) []hotTagItem {
	if len(tagUsageMap) == 0 {
		return nil
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

	n := 12
	if len(sorted) < n {
		n = len(sorted)
	}

	result := make([]hotTagItem, 0, n)
	for _, tc := range sorted[:n] {
		result = append(result, hotTagItem{Tag: tc.Tag, Count: tc.Count})
	}
	return result
}

func buildTagsInfoString(hotTags []hotTagItem, lang string) string {
	if len(hotTags) == 0 {
		return ""
	}

	prefix := i18n.TL(lang, "portrait_hot_tags_prefix")
	var parts []string
	for _, ht := range hotTags {
		parts = append(parts, fmt.Sprintf("%s(%d次)", ht.Tag, ht.Count))
	}
	return prefix + strings.Join(parts, "、")
}
