package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/internal/local/store"
	"BrainForever/toolset"
)

// ============================================================
// Portrait generation handler — GET /api/user/portrait?retouch=N
//
// Flow:
//  1. Frontend sends GET /api/user/portrait?retouch=3
//  2. Local-server reads ALL personal traits from user-specific traits DB
//  3. Local-server calls remote-server's POST /api/portrait (streaming)
//  4. Remote-server streams LLM portrait text via SSE
//  5. Local-server proxies SSE stream back to frontend
//
// Traits DB naming (same as trait extraction):
//   - Anonymous: localdb/anonymous.brain.db
//   - Logged-in: localdb/{userNo}.traits.db
// ============================================================

// portraitTraitItem represents a single trait sent to remote-server.
type portraitTraitItem struct {
	Text       string `json:"text"`
	Category   int    `json:"category"`
	Confidence int    `json:"confidence"`
	HalfLife   int    `json:"half_life"`
	CreateAt   string `json:"create_at"`
}

// portraitRemoteRequest is the request sent to remote-server.
type portraitRemoteRequest struct {
	Lang    string              `json:"lang"`
	Retouch int                 `json:"retouch"`
	Traits  []portraitTraitItem `json:"traits"`
}

// OnGetUserPortrait handles GET /api/user/portrait — reads all personal traits
// from the user's traits database, calls remote-server's portrait API,
// and proxies the SSE stream back to the frontend.
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

	// Determine user language from request
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))

	vs, err := session.ensureTraitsStore()
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_traits_store_unavailable"), http.StatusInternalServerError)
		return
	}

	// Read all traits from the user's traits database (ordered by create_at DESC)
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
	// 3. Convert traits to portrait request format
	// ----------------------------------------------------------
	traitItems := make([]portraitTraitItem, 0, len(allTraits))
	for _, t := range allTraits {
		traitItems = append(traitItems, portraitTraitItem{
			Text:       t.Trait,
			Category:   t.Category,
			Confidence: t.Confidence,
			HalfLife:   t.HalfLife,
			CreateAt:   t.CreateAt.Format(time.RFC3339),
		})
	}

	// ----------------------------------------------------------
	// 4. Call remote-server portrait API (SSE streaming)
	// ----------------------------------------------------------
	acceptLang := r.Header.Get("Accept-Language")
	if acceptLang == "" {
		acceptLang = "zh-CN"
	}

	remoteResp, err := callPortraitRemote(r.Context(), &portraitRemoteRequest{
		Lang:    acceptLang,
		Retouch: retouch,
		Traits:  traitItems,
	})
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_portrait_service_unavailable", map[string]interface{}{"Error": err.Error()}), http.StatusBadGateway)
		return
	}
	defer remoteResp.Body.Close()

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

	// ----------------------------------------------------------
	// 6. Compute portrait info (essence area metadata) and send
	//    as 'info' SSE event before proxying the remote stream.
	// ----------------------------------------------------------
	info := computePortraitInfo(allTraits, retouch)
	if infoJSON, err := json.Marshal(info); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", infoJSON)
		flusher.Flush()
	}

	// ----------------------------------------------------------
	// 7. Proxy SSE stream from remote-server to frontend
	//
	// Use sse.Reader to read each "data: ..." line from the
	// remote-server's SSE response and forward it verbatim
	// to the frontend.
	// ----------------------------------------------------------
	remoteSSE := sse.NewSSEReader(remoteResp.Body)

	for {
		data, ok := remoteSSE.Decode()
		if !ok {
			break
		}

		// Forward the raw SSE data line to the frontend
		_, writeErr := fmt.Fprintf(w, "data: %s\n\n", data)
		if writeErr != nil {
			// Frontend disconnected, stop proxying
			break
		}
		flusher.Flush()

		// Check for context cancellation
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}

	// Drain any remaining body
	io.Copy(io.Discard, remoteResp.Body)
}

// ============================================================
// portraitInfo — extra metadata sent as 'info' SSE event
// ============================================================

// portraitInfo is the SSE event wrapper for essence area metadata.
type portraitInfo struct {
	Event string           `json:"event"`
	Data  portraitInfoData `json:"data"`
}

// portraitInfoData carries additional metadata for the essence area display.
type portraitInfoData struct {
	GeneratedAt  string `json:"generated_at"`  // Generation timestamp
	ChatCount    int    `json:"chat_count"`    // Number of unique conversations
	TraitCount   int    `json:"trait_count"`   // Number of personal traits
	SpanDays     int    `json:"span_days"`     // Time span in days
	EarliestDate string `json:"earliest_date"` // Earliest message date (YYYY-MM-DD)
	LatestDate   string `json:"latest_date"`   // Latest message date (YYYY-MM-DD)
	Retouch      int    `json:"retouch"`       // Polish level
}

// computePortraitInfo computes the essence-area metadata from traits.
// allTraits is ordered by create_at DESC (newest first per ListAllTraitsByCreateTime),
// so the first element is the latest and the last is the earliest.
func computePortraitInfo(allTraits []store.PersonalTrait, retouch int) portraitInfo {
	// Count unique conversations
	chatSNSet := make(map[string]struct{})
	for _, t := range allTraits {
		if t.ChatSN != "" {
			chatSNSet[t.ChatSN] = struct{}{}
		}
	}
	chatCount := len(chatSNSet)

	// Time span: allTraits[0] = newest (latest), allTraits[n-1] = oldest (earliest)
	earliestStr := ""
	latestStr := ""
	spanDays := 0

	n := len(allTraits)
	if n > 0 {
		latest := allTraits[0].CreateAt     // newest first
		earliest := allTraits[n-1].CreateAt // oldest last
		earliestStr = earliest.Format("2006-01-02")
		latestStr = latest.Format("2006-01-02")

		// Truncate to date boundaries to avoid time-of-day interference,
		// then +1 for inclusive counting (e.g. 06/23~06/25 → 3 days).
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
		},
	}
}

// ============================================================
// Helpers
// ============================================================

// callPortraitRemote sends a portrait request to the remote-server
// and returns the SSE response body for streaming.
func callPortraitRemote(_ interface{}, req *portraitRemoteRequest) (*http.Response, error) {
	remoteURL := os.Getenv("REMOTE_SERVER_URL")
	if remoteURL == "" {
		remoteURL = "http://localhost:8088"
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	// Use a context that allows long-lived streaming connections
	httpReq, err := http.NewRequest(http.MethodPost, remoteURL+"/api/portrait", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// Use a streaming-friendly HTTP client with a long timeout
	client := &http.Client{Timeout: 5 * time.Minute}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("remote-server call failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("remote-server returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	return httpResp, nil
}
