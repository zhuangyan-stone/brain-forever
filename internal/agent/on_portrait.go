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

// maxTraitsForPortrait is the maximum number of personal traits sent to the
// portrait LLM. Older traits beyond this limit are omitted.
const maxTraitsForPortrait = 500

// maxExcerptsForPortrait is the maximum number of user quote excerpts sent to
// the portrait LLM as reference material.
const maxExcerptsForPortrait = 200

// portraitCacheTTL is the duration for which a cached portrait is considered
// valid. Within this period, the cached version is replayed instead of calling
// the LLM again.
const portraitCacheTTL = 30 * 24 * time.Hour

// ============================================================
// Portrait types
// ============================================================

type portraitTraitItem struct {
	Text       string `json:"text"`
	Category   int    `json:"category"`
	Confidence int    `json:"confidence"`
	HalfLife   int    `json:"half_life"`
	CreateAt   string `json:"create_at"`
}

func (t portraitTraitItem) ToString(lang string, index int) string {
	catName := i18n.TL(lang, fmt.Sprintf("trait_cat_%d", t.Category))
	catDesc := i18n.TL(lang, fmt.Sprintf("trait_cat_desc_%d", t.Category))

	hlName := i18n.TL(lang, fmt.Sprintf("trait_halflife_%d", t.HalfLife))
	hlDesc := i18n.TL(lang, fmt.Sprintf("trait_halflife_desc_%d", t.HalfLife))

	confLevel := i18n.TL(lang, "trait_confidence_"+confidenceLevelKey(t.Confidence))

	return i18n.TL(lang, "trait_item_format", map[string]any{
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

type portraitChatTitleItem struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	CrateAt string `json:"create_at"`
}

// tryListUserTraits reads the user's personal traits from the store.
// Caps the result at maxTraitsForPortrait (latest first). On read error or
// empty result, writes an error response and returns ok=false.
func (h *ChatAgent) tryListUserTraits(w http.ResponseWriter, lang string, userID int64) (allTraits []store.PersonalTrait, ok bool) {
	allTraits, err := theBrainStore.ListAllTraitsByCreateTime(userID)
	if err != nil {
		toolset.WriteError(w, i18n.TL(lang, "api_error_failed_to_read_traits", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return nil, false
	}

	if len(allTraits) == 0 {
		toolset.WriteError(w, i18n.TL(lang, "api_error_no_traits_data"), http.StatusNotFound)
		return nil, false
	}

	// Keep only the latest maxTraitsForPortrait entries.
	if len(allTraits) > maxTraitsForPortrait {
		allTraits = allTraits[:maxTraitsForPortrait]
	}

	return allTraits, true
}

func getRetouch(r *http.Request) int {
	retouchStr := r.URL.Query().Get("retouch")
	retouch := 3
	if retouchStr != "" {
		if v, err := strconv.Atoi(retouchStr); err == nil && v >= 0 && v <= 5 {
			retouch = v
		}
	}
	return retouch
}

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
		sb.WriteString(i18n.TL(lang, "chat_title_item_format", map[string]any{
			"Index":    i + 1,
			"Title":    item.Title,
			"CreateAt": item.CrateAt,
		}))
	}
	return sb.String()
}

type hotTagItem struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ============================================================
// Portrait SSE types
// ============================================================

type ssePortraitEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

func sendPortraitSSE(sw *sse.Writer, eventType string, data any) {
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

// flushAndDrainBody flushes the SSE writer and drains the HTTP request body.
// Ensures the client receives pending SSE events and the HTTP connection can
// be reused after the handler returns.
func flushAndDrainBody(flusher http.Flusher, r *http.Request) {
	flusher.Flush()
	io.Copy(io.Discard, r.Body)
}

type PortraitHighlights struct {
	CoreTraits    []string `json:"core_traits"`
	KeyHighlights []string `json:"key_highlights"`
}

// ============================================================
// OnGetUserPortrait -- GET /api/user/portrait handler
// ============================================================

func (h *ChatAgent) OnGetUserPortrait(w http.ResponseWriter, r *http.Request) {
	retouch := getRetouch(r)
	regen := r.URL.Query().Get("regen") == "true"

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))

	// ---- 1. Check cache (unless regen=true) ----
	if !regen {
		cached, err := thePortraitStore.GetLatestPortrait(sess.User.ID)
		if err == nil && cached != nil {
			if time.Since(cached.CreatedAt) <= portraitCacheTTL {
				h.replayPortraitFromCache(w, cached, lang)
				return
			}
		}
	}

	// ---- 2. Read traits ----
	allTraits, ok := h.tryListUserTraits(w, lang, sess.User.ID)
	if !ok {
		return
	}

	// ---- 3. Build LLM messages ----
	llmMsgs, hotTags, err := h.preparePortraitLLMMessages(allTraits, lang, sess.User.ID, retouch)
	if err != nil {
		toolset.WriteError(w, i18n.TL(lang, "api_error_failed_to_list_recent_chat_titles",
			map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	// ---- 4. Setup SSE response ----
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	sw := sse.NewSSEWriter(w)

	client := sessionLLMClient(sess)
	llmApiSettings := sessionLLMApiSetting(sess)

	// ---- 5. Send info event ----
	info := computePortraitInfo(allTraits, retouch, hotTags)
	if infoJSON, err := json.Marshal(info); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", infoJSON)
		flusher.Flush()
	}

	// ---- 6. Stream portrait content ----
	totalContent, ok := h.runPortraitLLMStream(r.Context(), sw, flusher, client, llmMsgs, llmApiSettings.ApiKey, r)
	if !ok {
		return
	}

	// ---- 7. Extract highlights ----
	var meta *PortraitHighlights
	if totalContent != "" {
		meta = extractPortraitHighlights(r.Context(), client, lang, totalContent, llmApiSettings.ApiKey)
		if meta != nil {
			sendPortraitSSE(sw, "meta", meta)
			flusher.Flush()
		}
	}

	// ---- 8. Generate title internally ----
	portraitTitle := generatePortraitTitle(r.Context(), client, lang, totalContent, llmApiSettings.ApiKey)
	if portraitTitle != "" {
		sendPortraitSSE(sw, "title", portraitTitle)
		flusher.Flush()
	}

	// ---- 9. Persist to DB (best-effort) ----
	if totalContent != "" {
		h.persistPortrait(sess.User.ID, portraitTitle, totalContent, info.Data, meta, hotTags)
	}

	// ---- 10. Done ----
	sendPortraitSSE(sw, "done", map[string]any{})
	flusher.Flush()
}

// streamPortraitContent reads the LLM streaming response and sends text chunks
// as SSE events. Returns the concatenated content. Returns ok=false if the
// request was cancelled.
// runPortraitLLMStream creates an LLM streaming request, starts the stream,
// reads all content via streamPortraitContent, and checks post-stream errors.
// Returns ok=false if any stream error occurred.
func (h *ChatAgent) runPortraitLLMStream(
	ctx context.Context,
	sw *sse.Writer,
	flusher http.Flusher,
	client llm.Client,
	llmMsgs []llm.Message,
	apiKey string,
	r *http.Request,
) (totalContent string, ok bool) {
	streamReq := llm.ChatCompletionRequest{
		Model:    client.Model(),
		Messages: llmMsgs,
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	streamReq.IncludeUsage(true)

	stream := client.ChatStreamWithOptions(ctx, streamReq, apiKey)
	if err := stream.Err(); err != nil {
		sendPortraitSSE(sw, "error", fmt.Sprintf("LLM stream failed. %v", err))
		flushAndDrainBody(flusher, r)
		return "", false
	}
	defer stream.Close()

	totalContent, ok = h.streamPortraitContent(ctx, sw, flusher, stream, client)
	if !ok {
		return "", false
	}

	if err := stream.Err(); err != nil {
		sendPortraitSSE(sw, "error", fmt.Sprintf("stream error. %v", err))
		flushAndDrainBody(flusher, r)
		return "", false
	}

	return totalContent, true
}

// streamPortraitContent iterates over the LLM streaming response chunks,
// sending each non-empty text delta as an SSE "text" event and flushing
// after every chunk. Concatenates and returns the full response content.
// Returns ok=false if the request context is cancelled.
func (h *ChatAgent) streamPortraitContent(
	ctx context.Context,
	sw *sse.Writer,
	flusher http.Flusher,
	stream *llm.ChatCompletionChunkDecoder,
	client llm.Client,
) (totalContent string, ok bool) {
	for stream.Next() {
		chunk := stream.CurrentChatCompletionChunk()

		if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
			client.SetUsageInfo(*chunk.Usage)
		}

		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			if content != "" {
				totalContent += content
				sendPortraitSSE(sw, "text", content)
			}
		}

		select {
		case <-ctx.Done():
			sendPortraitSSE(sw, "error", "request cancelled")
			return "", false
		default:
		}

		flusher.Flush()
	}
	return totalContent, true
}

// preparePortraitLLMMessages reads hot tags, recent chat titles, and excerpt
// data, then builds the system and user messages for the portrait LLM call.
func (h *ChatAgent) preparePortraitLLMMessages(
	allTraits []store.PersonalTrait,
	lang string,
	userID int64,
	retouch int,
) (llmMsgs []llm.Message, hotTags []hotTagItem, err error) {
	tagUsageMap, _ := theChatStore.SelectNonEmptyTagsGroup()
	hotTags = formatHotTags(tagUsageMap)
	tagsInfoStr := buildTagsInfoString(hotTags, lang)

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

	recentChatTitles, err := theChatStore.ListChatTitles(userID, 100)
	if err != nil {
		return nil, nil, err
	}

	chatTitleItems := make([]portraitChatTitleItem, 0, len(recentChatTitles))
	for _, t := range recentChatTitles {
		chatTitleItems = append(chatTitleItems, portraitChatTitleItem{
			ID:      t.ID,
			Title:   t.Title,
			CrateAt: t.CrateAt.Local().Format(time.RFC3339),
		})
	}

	traitsDesc := formatTraitItems(traitItems, lang)
	chatTitlesStr := formatChatTitles(chatTitleItems, lang)

	// ----- Read excerpt data for portrait enrichment -----
	excerptStatsStr := ""
	recentExcerptsStr := ""

	valueTypeStats, statsErr := theExcerptStore.CountExcerptsByValueTypes(userID)
	if statsErr == nil && len(valueTypeStats) > 0 {
		excerptStatsStr = buildExcerptStatsString(valueTypeStats, lang)
	}

	latestExcerpts, excerptsErr := theExcerptStore.ListLatestExcerpts(userID, maxExcerptsForPortrait)
	if excerptsErr == nil && len(latestExcerpts) > 0 {
		recentExcerptsStr = formatExcerptsForPortrait(latestExcerpts, lang)
	}

	systemContent := i18n.SystemPrompt.TL(lang, "portrait", map[string]any{
		"Retouch":          retouch,
		"TraitsJSON":       traitsDesc,
		"TagsInfo":         tagsInfoStr,
		"RecentChatTitles": chatTitlesStr,
		"ExcerptStats":     excerptStatsStr,
		"RecentExcerpts":   recentExcerptsStr,
		"CurrentLocalTime": time.Now().In(time.Local).Format("2006-01-02 15:04:05 (MST)"),
	})

	userContent := i18n.SystemPrompt.TL(lang, "portrait_user_prompt", map[string]any{
		"Retouch": retouch,
	})

	llmMsgs = []llm.Message{
		{Role: llm.RoleSystem, Content: systemContent},
		{Role: llm.RoleUser, Content: userContent},
	}

	return llmMsgs, hotTags, nil
}

func extractPortraitHighlights(ctx context.Context, client llm.Client, lang, portraitText string, apiKey string) *PortraitHighlights {
	systemContent := i18n.SystemPrompt.TL(lang, "highlights", map[string]any{
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

	resp, err := client.ChatWithOptions(ctx, req, apiKey)
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

type portraitInfo struct {
	Event string           `json:"event"`
	Data  portraitInfoData `json:"data"`
}

type portraitInfoData struct {
	GeneratedAt     string       `json:"generated_at"`
	NextGeneratedAt string       `json:"next_generated_at"`
	ChatCount       int          `json:"chat_count"`
	TraitCount      int          `json:"trait_count"`
	SpanDays        int          `json:"span_days"`
	EarliestDate    string       `json:"earliest_date"`
	LatestDate      string       `json:"latest_date"`
	Retouch         int          `json:"retouch"`
	HotTags         []hotTagItem `json:"hot_tags"`
}

func computePortraitInfo(allTraits []store.PersonalTrait, retouch int, hotTags []hotTagItem) portraitInfo {
	chatIDSet := make(map[int64]struct{})
	for _, t := range allTraits {
		if t.ChatID != 0 {
			chatIDSet[t.ChatID] = struct{}{}
		}
	}
	chatCount := len(chatIDSet)

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
			GeneratedAt:     time.Now().Format("2006-01-02 15:04:05"),
			NextGeneratedAt: time.Now().Add(portraitCacheTTL + time.Second).Format("2006-01-02 15:04:05"),
			ChatCount:       chatCount,
			TraitCount:      n,
			SpanDays:        spanDays,
			EarliestDate:    earliestStr,
			LatestDate:      latestStr,
			Retouch:         retouch,
			HotTags:         hotTags,
		},
	}
}

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
		parts = append(parts, fmt.Sprintf("%s(%d)", ht.Tag, ht.Count))
	}
	return prefix + strings.Join(parts, ", ")
}

// buildExcerptStatsString formats the excerpt value type distribution into a
// human-readable string for the portrait LLM prompt.
func buildExcerptStatsString(valueTypeStats map[int16]int, lang string) string {
	if len(valueTypeStats) == 0 {
		return ""
	}

	// Sort value type IDs for deterministic output.
	ids := make([]int16, 0, len(valueTypeStats))
	for id := range valueTypeStats {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var sb strings.Builder
	sb.WriteString(i18n.TL(lang, "portrait_excerpt_stats_header"))
	for i, id := range ids {
		valueName := theExcerptVDCache.GetValueByID(id)
		if valueName == "" {
			continue
		}
		localized := i18n.TL(lang, "excerpt_value_type_"+valueName)
		if i > 0 {
			sb.WriteString("、")
		}
		sb.WriteString(fmt.Sprintf("%s(%d)", localized, valueTypeStats[id]))
	}
	return sb.String()
}

// formatExcerptsForPortrait formats a slice of Excerpt into a readable string
// for the portrait LLM, including content, context summary, reason, value types,
// and message time for each excerpt.
func formatExcerptsForPortrait(excerpts []store.Excerpt, lang string) string {
	if len(excerpts) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, e := range excerpts {
		if i > 0 {
			sb.WriteString("\n---\n")
		}

		// Map value IDs to localized names.
		typeNames := make([]string, 0, len(e.Values))
		for _, vid := range e.Values {
			if v := theExcerptVDCache.GetValueByID(vid); v != "" {
				localized := i18n.TL(lang, "excerpt_value_type_"+v)
				typeNames = append(typeNames, localized)
			}
		}

		content := i18n.TL(lang, "portrait_excerpt_item_format", map[string]any{
			"Index":          i + 1,
			"Content":        e.Content,
			"ContextSummary": e.ContextSummary,
			"Reason":         e.Reason,
			"ValueTypes":     strings.Join(typeNames, ", "),
			"MsgTime":        e.MsgTime.Local().Format("2006-01-02 15:04"),
		})
		sb.WriteString(content)
	}
	return sb.String()
}

// ============================================================
// Portrait caching and persistence helpers
// ============================================================

// replayPortraitFromCache replays a cached UserPortrait as SSE events so the
// frontend receives the same event sequence as a fresh LLM generation.
func (h *ChatAgent) replayPortraitFromCache(w http.ResponseWriter, p *store.UserPortrait, lang string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	sw := sse.NewSSEWriter(w)

	// Resolve user name (best-effort from Alpine store won't be available here;
	// we just send the cached data with original generated_at).

	// ---- info event ----
	var hotTags []hotTagItem
	if len(p.HotTags) > 0 {
		json.Unmarshal(p.HotTags, &hotTags)
	}
	infoData := portraitInfoData{
		GeneratedAt:     p.CreatedAt.Format("2006-01-02 15:04:05"),
		NextGeneratedAt: p.CreatedAt.Add(portraitCacheTTL + time.Second).Format("2006-01-02 15:04:05"),
		ChatCount:       p.ChatCount,
		TraitCount:      p.TraitCount,
		SpanDays:        p.SpanDays,
		Retouch:         p.Retouch,
		HotTags:         hotTags,
	}
	if p.EarliestDate != nil {
		infoData.EarliestDate = p.EarliestDate.Format("2006-01-02")
	}
	if p.LatestDate != nil {
		infoData.LatestDate = p.LatestDate.Format("2006-01-02")
	}
	info := portraitInfo{Event: "info", Data: infoData}
	if infoJSON, err := json.Marshal(info); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", infoJSON)
		flusher.Flush()
	}

	// ---- text event ----
	if p.Content != "" {
		sendPortraitSSE(sw, "text", p.Content)
		flusher.Flush()
	}

	// ---- meta event ----
	var coreTraits, keyHighlights []string
	json.Unmarshal(p.CoreTraits, &coreTraits)
	json.Unmarshal(p.KeyHighlights, &keyHighlights)
	if len(coreTraits) > 0 || len(keyHighlights) > 0 {
		meta := PortraitHighlights{
			CoreTraits:    coreTraits,
			KeyHighlights: keyHighlights,
		}
		sendPortraitSSE(sw, "meta", meta)
		flusher.Flush()
	}

	// ---- title event ----
	if p.Title != "" {
		sendPortraitSSE(sw, "title", p.Title)
		flusher.Flush()
	}

	// ---- done event ----
	sendPortraitSSE(sw, "done", map[string]any{})
	flusher.Flush()
}

// generatePortraitTitle calls the LLM to generate a concise title for the
// portrait content. Returns empty string on failure.
func generatePortraitTitle(ctx context.Context, client llm.Client, lang, content, apiKey string) string {
	if content == "" {
		return ""
	}

	systemContent := i18n.SystemPrompt.TL(lang, "doc_title", nil)
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemContent},
		{Role: llm.RoleUser, Content: content},
	}

	titleCtx, titleCancel := context.WithTimeout(ctx, 30*time.Second)
	defer titleCancel()

	resp, err := client.Chat(titleCtx, messages, apiKey)
	if err != nil {
		return ""
	}

	title := ""
	if len(resp.Choices) > 0 {
		title = resp.Choices[0].Message.Content
	}

	const maxTitleLen = 50
	if title == "" || toolset.VisualLength(title) > maxTitleLen {
		return ""
	}

	return title
}

// persistPortrait saves the complete portrait result to portraits.
// Errors are silently ignored (best-effort persistence).
func (h *ChatAgent) persistPortrait(userID int64, title, content string,
	info portraitInfoData, meta *PortraitHighlights, hotTags []hotTagItem) {

	// Marshal JSONB fields.
	coreTraitsJSON := mustMarshalJSON(func() any {
		if meta != nil {
			return meta.CoreTraits
		}
		return []string{}
	}())
	keyHighlightsJSON := mustMarshalJSON(func() any {
		if meta != nil {
			return meta.KeyHighlights
		}
		return []string{}
	}())
	hotTagsJSON := mustMarshalJSON(hotTags)

	// Compute hottest tag.
	hottestTag := ""
	hottestTagCount := 0
	for _, ht := range hotTags {
		if ht.Count > hottestTagCount {
			hottestTagCount = ht.Count
			hottestTag = ht.Tag
		}
	}

	// Parse dates.
	var earliestDate, latestDate *time.Time
	if info.EarliestDate != "" {
		if t, err := time.Parse("2006-01-02", info.EarliestDate); err == nil {
			earliestDate = &t
		}
	}
	if info.LatestDate != "" {
		if t, err := time.Parse("2006-01-02", info.LatestDate); err == nil {
			latestDate = &t
		}
	}

	record := &store.UserPortrait{
		UserID:          userID,
		Title:           title,
		Content:         content,
		CoreTraits:      coreTraitsJSON,
		KeyHighlights:   keyHighlightsJSON,
		HotTags:         hotTagsJSON,
		HottestTag:      hottestTag,
		HottestTagCount: hottestTagCount,
		ChatCount:       info.ChatCount,
		TraitCount:      info.TraitCount,
		SpanDays:        info.SpanDays,
		EarliestDate:    earliestDate,
		LatestDate:      latestDate,
		Retouch:         info.Retouch,
	}

	if _, err := thePortraitStore.InsertPortrait(record); err != nil {
		// Non-critical: log but don't disrupt the user experience.
		// The logger is accessed via the store's own logger setup.
	}
}

// mustMarshalJSON is a convenience wrapper for json.Marshal that returns a
// json.RawMessage. Panics only on programmer error (unmarshallable type).
func mustMarshalJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("[]")
	}
	return json.RawMessage(data)
}
