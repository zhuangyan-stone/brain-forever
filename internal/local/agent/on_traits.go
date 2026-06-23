package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"BrainForever/internal/local/store"
)

// ============================================================
// Trait extraction handler — POST /api/chat/traits
//
// Flow:
//  1. Frontend sends POST /api/chat/traits with {"sn": "xxx"}
//  2. Local-server reads chat messages from local DB
//  3. Local-server calls remote-server's POST /api/traits
//  4. Remote-server extracts traits via LLM and returns JSON
//  5. Local-server embeds each trait via the embedder and stores
//     traits + keywords into the user-specific traits DB
//  6. Local-server returns the result to the frontend
//
// Traits DB naming:
//   - Anonymous: localdb/anonymous.brain.db
//   - Logged-in: localdb/{userNo}.brain.db
// ============================================================

// traitsFrontendRequest is the request from the frontend to local-server.
type traitsFrontendRequest struct {
	SN string `json:"sn"`
}

// traitsMsg is a single message sent to the remote-server.
type traitsMsg struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	CreateAt string `json:"create_at"`
}

// traitsRemoteRequest is the request sent to remote-server.
type traitsRemoteRequest struct {
	SN                    string      `json:"sn"`
	Title                 string      `json:"title"`
	Messages              []traitsMsg `json:"messages"`
	ExistingTraitsSummary string      `json:"existing_traits_summary,omitempty"` // Traits already extracted (for incremental extraction)
}

// traitsFeature is a single extracted feature from remote-server response.
type traitsFeature struct {
	CategoryID   int             `json:"category_id"`
	CategoryName string          `json:"category_name"`
	FeatureText  string          `json:"feature_text"`
	Keywords     []traitsKeyword `json:"keywords"`
	Confidence   int             `json:"confidence"`
	HalfLife     string          `json:"half_life"`
}

type traitsKeyword struct {
	Type string `json:"type"`
	Word string `json:"word"`
}

// traitsRemoteResponse is the response from remote-server.
type traitsRemoteResponse struct {
	Features []traitsFeature `json:"features,omitempty"`
	Usage    interface{}     `json:"usage,omitempty"`
	Error    string          `json:"error,omitempty"`

	// Extraction state — returned to frontend so it can update the
	// chat list without needing a separate API call or client-side guess.
	ExtractedAt           *string `json:"extracted_at,omitempty"`
	ExtractedMessageCount int     `json:"extracted_message_count,omitempty"`
}

// halfLifeToInt converts the half-life string from the remote-server to an integer.
// short=1, medium=2, long=3, permanent=4.
func halfLifeToInt(s string) int {
	switch s {
	case "short":
		return 1
	case "medium":
		return 2
	case "long":
		return 3
	case "permanent":
		return 4
	default:
		return 2 // default to medium
	}
}

// keywordTypeToInt converts keyword type letter (A-F) to integer (1-6).
func keywordTypeToInt(t string) int {
	switch t {
	case "A":
		return 1
	case "B":
		return 2
	case "C":
		return 3
	case "D":
		return 4
	case "E":
		return 5
	case "F":
		return 6
	default:
		return 4 // default to D
	}
}

// userTraitsDBPath returns the traits database path for the given user.
// Anonymous users get "localdb/anonymous.brain.db".
// Logged-in users get "localdb/{userNo}.brain.db".
func userTraitsDBPath(userNo string) string {
	if userNo == "" {
		return "localdb/anonymous.brain.db"
	}
	return "localdb/" + userNo + ".traits.db"
}

// ensureTraitsStore returns the session's traitsStore, or an error if it was
// not created (e.g., due to a failure during eager initialization).
// The store is now created eagerly at session creation or user switch time.
func (s *session) ensureTraitsStore() (*store.VectorStore, error) {
	if s.traitsStore != nil {
		return s.traitsStore, nil
	}
	return nil, fmt.Errorf("traits store not available (failed during initialization)")
}

// OnExtractTraits handles POST /api/chat/traits — accepts a chat SN,
// reads the chat messages from the local database, then calls the
// remote-server's trait extraction API, embeds and stores the results,
// and returns the features to the frontend.
func (h *ChatAgent) OnExtractTraits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// ----------------------------------------------------------
	// 1. Parse request body
	// ----------------------------------------------------------
	var req traitsFrontendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %v"}`, err), http.StatusBadRequest)
		return
	}

	if req.SN == "" {
		http.Error(w, `{"error":"missing 'sn' field"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[traits] frontend request: sn=%s", req.SN)

	// ----------------------------------------------------------
	// 2. Resolve session and find the chat
	// ----------------------------------------------------------
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	// Find the chat by SN in the session's chat list
	var foundChat *store.Chat
	session.chatsMu.Lock()
	for i := range session.chats {
		if session.chats[i].SN == req.SN {
			foundChat = &session.chats[i]
			break
		}
	}
	chatsStore := session.chatsStore
	session.chatsMu.Unlock()

	if foundChat == nil {
		http.Error(w, fmt.Sprintf(`{"error":"chat not found (sn=%s)"}`, req.SN), http.StatusNotFound)
		return
	}

	// ----------------------------------------------------------
	// 3. Read messages from database
	// ----------------------------------------------------------
	dbMessages, err := chatsStore.ListMessages(foundChat.ID)
	if err != nil {
		log.Printf("[traits] list messages failed: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":"list messages failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	if len(dbMessages) == 0 {
		http.Error(w, `{"error":"no messages in this chat"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[traits] read %d messages from DB for sn=%s", len(dbMessages), req.SN)

	// ----------------------------------------------------------
	// 3.1 Early short-circuit: if all messages have already been
	//     extracted, return empty result without calling LLM.
	// ----------------------------------------------------------
	if foundChat.ExtractedAt != nil && foundChat.ExtractedMessageCount >= len(dbMessages) {
		log.Printf("[traits] all %d messages already extracted for sn=%s, skipping LLM call",
			len(dbMessages), req.SN)
		extractedAtStr := foundChat.ExtractedAt.Format(time.RFC3339)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(traitsRemoteResponse{
			Features:              []traitsFeature{},
			ExtractedAt:           &extractedAtStr,
			ExtractedMessageCount: foundChat.ExtractedMessageCount,
		})
		return
	}

	// ----------------------------------------------------------
	// 4. Convert messages to traits request format
	// ----------------------------------------------------------
	msgs := make([]traitsMsg, 0, len(dbMessages))
	for _, m := range dbMessages {
		role := "user"
		if m.Role == 1 {
			role = "assistant"
		}

		content := m.Content
		if role == "assistant" {
			runes := []rune(content)
			if len(runes) > 1024 {
				content = string(runes[:500]) + "\n...\n" + string(runes[len(runes)-500:])
			}
		}

		createAt := ""
		if !m.CreateAt.IsZero() {
			createAt = m.CreateAt.Format("2006-01-02 15:04:05")
		}

		msgs = append(msgs, traitsMsg{
			Role:     role,
			Content:  content,
			CreateAt: createAt,
		})
	}

	// ----------------------------------------------------------
	// 4.1 Incremental extraction: only send new (un-extracted) messages
	// ----------------------------------------------------------
	maxGroupIndex := dbMessages[len(dbMessages)-1].GroupIndex
	if foundChat.ExtractedAt != nil && foundChat.ExtractedMessageCount < len(dbMessages) {
		msgs = msgs[foundChat.ExtractedMessageCount:]
		log.Printf("[traits] incremental extraction: skipping %d already-extracted messages, sending %d new messages",
			foundChat.ExtractedMessageCount, len(msgs))
	}

	log.Printf("[traits] extract sn=%s, messages=%d", req.SN, len(msgs))

	// ----------------------------------------------------------
	// 5. Call remote-server API
	// ----------------------------------------------------------
	remoteURL := os.Getenv("REMOTE_SERVER_URL")
	if remoteURL == "" {
		remoteURL = "http://localhost:8088"
	}

	remoteReq := traitsRemoteRequest{
		SN:       req.SN,
		Title:    foundChat.Title,
		Messages: msgs,
	}

	reqBody, err := json.Marshal(remoteReq)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"marshal request failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	log.Printf("[traits] calling remote-server: %s/api/traits with %d messages", remoteURL, len(msgs))

	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, remoteURL+"/api/traits", bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"create request failed: %v"}`, err), http.StatusInternalServerError)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Forward Accept-Language header for i18n
	if lang := r.Header.Get("Accept-Language"); lang != "" {
		httpReq.Header.Set("Accept-Language", lang)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[traits] remote-server call failed: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":"remote-server call failed: %v"}`, err), http.StatusInternalServerError)
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		// Try to read error response body
		var errResp traitsRemoteResponse
		if decodeErr := json.NewDecoder(httpResp.Body).Decode(&errResp); decodeErr == nil && errResp.Error != "" {
			http.Error(w, fmt.Sprintf(`{"error":"remote-server error: %s"}`, errResp.Error), http.StatusInternalServerError)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error":"remote-server returned status %d"}`, httpResp.StatusCode), http.StatusInternalServerError)
		return
	}

	// ----------------------------------------------------------
	// 6. Decode remote-server response
	// ----------------------------------------------------------
	var remoteResp traitsRemoteResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&remoteResp); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"decode remote-server response failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// ----------------------------------------------------------
	// 7. Embed each trait and store into user-specific traits DB
	// ----------------------------------------------------------
	if len(remoteResp.Features) > 0 {
		if err := h.storeTraitsInSession(r.Context(), session, remoteResp.Features, foundChat.SN); err != nil {
			log.Printf("[traits] store traits failed (non-fatal): %v", err)
			// Non-fatal: still return features to frontend even if storage fails
		}
	}

	// ----------------------------------------------------------
	// 8. Update extraction progress
	// ----------------------------------------------------------
	if err := chatsStore.UpdateExtractionProgress(foundChat.ID, len(dbMessages)); err != nil {
		log.Printf("[traits] update extraction progress failed (non-fatal): %v", err)
	} else {
		// Sync in-memory struct to match DB — this ensures the
		// early short-circuit check works on subsequent calls
		// within the same session without needing a reload.
		now := time.Now()
		foundChat.ExtractedAt = &now
		foundChat.ExtractedMessageCount = len(dbMessages)
	}
	if err := chatsStore.MarkMessagesExtracted(foundChat.ID, maxGroupIndex); err != nil {
		log.Printf("[traits] mark messages extracted failed (non-fatal): %v", err)
	}
	log.Printf("[traits] extraction progress updated: sn=%s, msg_count=%d", req.SN, len(dbMessages))

	// ----------------------------------------------------------
	// 9. Populate extraction state in response, then return
	// ----------------------------------------------------------
	if foundChat.ExtractedAt != nil {
		extractedAtStr := foundChat.ExtractedAt.Format(time.RFC3339)
		remoteResp.ExtractedAt = &extractedAtStr
		remoteResp.ExtractedMessageCount = foundChat.ExtractedMessageCount
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(remoteResp)

	log.Printf("[traits] completed for sn=%s, features=%d", req.SN, len(remoteResp.Features))
}

// storeTraitsInSession embeds each trait feature and stores it along with keywords
// into the session's per-user traits database.
// chatSN is the source chat SN (chat_sessions.sn), stored in the trait record for traceability.
func (h *ChatAgent) storeTraitsInSession(ctx context.Context, session *session, features []traitsFeature, chatSN string) error {
	emb := h.embedder

	// The traits store was already created eagerly at session creation or user switch time
	vs, err := session.ensureTraitsStore()
	if err != nil {
		return fmt.Errorf("ensure traits store: %w", err)
	}

	for _, f := range features {
		if f.FeatureText == "" {
			continue
		}

		// Embed the feature text
		vector, err := emb.Embed(ctx, f.FeatureText)
		if err != nil {
			log.Printf("[traits] embed failed for '%s': %v", f.FeatureText, err)
			continue
		}

		// Store the trait (with source chat_sn)
		trait := &store.PersonalTrait{
			Trait:      f.FeatureText,
			Category:   f.CategoryID,
			Confidence: f.Confidence,
			HalfLife:   halfLifeToInt(f.HalfLife),
			ChatSN:     chatSN,
		}

		traitID, err := vs.AddTrait(ctx, trait, vector)
		if err != nil {
			log.Printf("[traits] add trait failed for '%s': %v", f.FeatureText, err)
			continue
		}

		log.Printf("[traits] stored trait id=%d: '%s' (cat=%d, conf=%d, half=%s) in %s",
			traitID, f.FeatureText, f.CategoryID, f.Confidence, f.HalfLife,
			userTraitsDBPath(session.userNo))

		// Store keywords
		for _, kw := range f.Keywords {
			if kw.Word == "" {
				continue
			}
			keyword := &store.TraitKeyword{
				Word:    kw.Word,
				Kind:    keywordTypeToInt(kw.Type),
				TraitID: traitID,
			}
			if _, err := vs.AddKeyword(keyword); err != nil {
				log.Printf("[traits] add keyword failed (trait_id=%d, word='%s'): %v",
					traitID, kw.Word, err)
			}
		}
	}

	return nil
}
