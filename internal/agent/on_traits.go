package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"BrainForever/infra/embedder"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/logger"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/toolset"
)

// ============================================================
// Trait extraction handler -POST /api/chat/traits
// ============================================================

type traitsFrontendRequest struct {
	SN string `json:"sn"`
}

type traitsMsg struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	CreateAt string `json:"create_at"`
}

type traitsFeature struct {
	CategoryID   int             `json:"category_id"`
	CategoryName string          `json:"category_name"`
	FeatureText  string          `json:"feature_text"`
	Keywords     []traitsKeyword `json:"keywords"`
	Confidence   int             `json:"confidence"`
	HalfLife     string          `json:"half_life"`
	PrivacyLevel string          `json:"privacy_level"`
}

type traitsKeyword struct {
	Type string `json:"type"`
	Word string `json:"word"`
}

type traitsResponse struct {
	Features []traitsFeature `json:"-"`
	Usage    any             `json:"usage,omitempty"`
	Error    string          `json:"error,omitempty"`

	ExtractedAt    *string `json:"extracted_at,omitempty"`
	ExtractedCount int     `json:"extracted_count,omitempty"`
	NewCount       int     `json:"new_count,omitempty"`
}

// ============================================================
// Exported types (for use by tasks package)
// ============================================================

// TraitFeature is an exported alias for traitsFeature used across packages.
type TraitFeature = traitsFeature

// TraitKeywordItem is an exported alias for traitsKeyword used across packages.
type TraitKeywordItem = traitsKeyword

// TraitResult is the result of an LLM trait extraction call.
// It mirrors traitsResponse but omits HTTP-specific fields.
type TraitResult struct {
	Features []TraitFeature
}

// HalfLifeToInt converts a half-life string ("short"/"medium"/"long"/"permanent") to int (1-4).
func HalfLifeToInt(s string) int {
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
		return 2
	}
}

// PrivacyLevelToInt converts a privacy-level string ("private"/"protected"/"public") to int (0-2).
func PrivacyLevelToInt(s string) int {
	switch s {
	case "private":
		return 0
	case "protected":
		return 1
	case "public":
		return 2
	default:
		return 1 // 默认 protected（保守选择）
	}
}

// KeywordTypeToInt converts a keyword type letter ("A"-"F") to int (1-6).
func KeywordTypeToInt(t string) int {
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
		return 4
	}
}

// OnExtractTraits handles POST /api/chat/traits
func (h *ChatAgent) OnExtractTraits(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))

	var req traitsFrontendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteError(w, i18n.TL(lang, "api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.SN == "" {
		toolset.WriteError(w, i18n.TL(lang, "api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	foundChat := sess.FindChatBySN(req.SN)
	if foundChat == nil {
		// Fallback: query database directly when the chat is not in the
		// session's in-memory list (e.g., session expired or recreated).
		dbChat, err := theChatStore.FindChatBySN(req.SN)
		if err != nil || dbChat.Deleted {
			toolset.WriteError(w, i18n.TL(lang, "api_error_chat_not_found"), http.StatusNotFound)
			return
		}
		// Add to in-memory list so subsequent lookups succeed.
		sess.AddChatToList(*dbChat)
		foundChat = dbChat
	}

	dbMessages, err := theChatStore.ListUnExtractMessages(foundChat.ID)
	if err != nil {
		toolset.WriteError(w, i18n.TL(lang, "api_error_failed_to_list_messages", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	if len(dbMessages) == 0 {
		handleNoNewMessages(w, foundChat, theChatStore)
		return
	}

	remoteResp, err := h.callTraitsLLM(r.Context(), foundChat.Title, dbMessages, lang, sess)
	if err != nil {
		toolset.WriteError(w, i18n.TL(lang, "api_error_internal"), http.StatusInternalServerError)
		return
	}

	lastMsgID := dbMessages[len(dbMessages)-1].ID

	var newCount int
	if len(remoteResp.Features) > 0 {
		storedCount, err := h.storeTraitsInSession(r.Context(), sess, remoteResp.Features, foundChat.ID, lastMsgID)
		if err != nil {
			h.logger.Errorf("store traits to brain.db failed (chatID=%d). %v", foundChat.ID, err)
			toolset.WriteError(w, i18n.TL(lang, "api_error_internal"), http.StatusInternalServerError)
			return
		}
		newCount = storedCount
		// StoreTraitsStandalone already atomically marks messages and updates extracted_at.
		// No separate UpdateMessagesExtracted / UpdateExtractionCountAndTime needed.
	} else {
		// No traits; mark messages as processed atomically.
		if _, err := theBrainStore.AddTraits(r.Context(), foundChat.ID, lastMsgID, nil); err != nil {
			h.logger.Errorf("complete extraction for chat %d failed. %v", foundChat.ID, err)
		}
	}

	remoteResp.NewCount = newCount

	if foundChat.ExtractedAt != nil {
		extractedAtStr := foundChat.ExtractedAt.Format(time.RFC3339)
		remoteResp.ExtractedAt = &extractedAtStr
		remoteResp.ExtractedCount = foundChat.ExtractedCount
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(remoteResp)
}

func (h *ChatAgent) callTraitsLLM(ctx context.Context, title string, dbMessages []store.Message, lang string, sess *session.Session) (*traitsResponse, error) {
	// Build messages with timestamps (HTTP handler adds time context).
	llmMsgs := buildTraitsLLMMessages(title, dbMessages, lang, true)
	if len(llmMsgs) <= 1 {
		return nil, fmt.Errorf("no valid messages after conversion")
	}

	client := sessionLLMClient(sess)
	apiSetting := sessionLLMApiSetting(sess)

	traitResult := callTraitsLLMWithTool(ctx, llmMsgs, lang, client, apiSetting.ApiKey)
	if traitResult == nil {
		return nil, fmt.Errorf("lLM call failed")
	}

	result := &traitsResponse{
		Features: traitResult.Features,
	}
	h.logger.Debugf("callTraitsLLM returning features=%d", len(result.Features))
	return result, nil
}

func getTraitSystemPrompt(lang string, chatTitle string) string {
	return i18n.SystemPrompt.TL(lang, "traits", map[string]any{
		"CurrentLocalTime": time.Now().In(time.Local).Format("2006-01-02 15:04:05 (MST)"),
		"ChatTitle":        chatTitle,
	})
}

func dbMessagesToTraitsMsgs(dbMessages []store.Message) (msgs []traitsMsg, lastMsgID int64) {
	count := len(dbMessages)
	msgs = make([]traitsMsg, 0, count)

	for _, m := range dbMessages {
		role := llm.RoleUser
		if m.Role == 1 {
			role = llm.RoleAssistant
		}

		content := m.Content
		if role == llm.RoleAssistant {
			runes := []rune(content)
			if len(runes) > 1024 {
				content = string(runes[:500]) + "...\n...\n..." + string(runes[len(runes)-500:])
			}
		}

		msgs = append(msgs, traitsMsg{
			Role:     role,
			Content:  content,
			CreateAt: toolset.FormatTimeWithLocation(m.CreateAt),
		})
	}

	lastMsgID = dbMessages[count-1].ID
	return
}

func (h *ChatAgent) storeTraitsInSession(ctx context.Context, sess *session.Session, features []traitsFeature, chatID int64, upToMsgID int64) (int, error) {
	emb := sessionEmbedder(sess)
	apiSetting := sessionEmbedderApiSetting(sess)
	return StoreTraitsStandalone(ctx, features, chatID, sess.User.ID, upToMsgID, emb, apiSetting.ApiKey, h.dedupEnabled, h.dedupThreshold)
}

func handleNoNewMessages(w http.ResponseWriter, foundChat *store.Chat, chatsStore *store.ChatStore) {
	resp := traitsResponse{
		Features: []traitsFeature{},
		NewCount: 0,
	}
	if foundChat.ExtractedAt != nil {
		extractedAtStr := foundChat.ExtractedAt.Format(time.RFC3339)
		resp.ExtractedAt = &extractedAtStr
	}
	if _, err := chatsStore.CountMessages(foundChat.ID); err == nil {
		resp.ExtractedCount = foundChat.ExtractedCount
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func updateExtractionProgress(foundChat *store.Chat, chatsStore *store.ChatStore, newTraitCount int) {
	if err := chatsStore.UpdateExtractionCountAndTime(foundChat.ID, newTraitCount); err == nil {
		now := time.Now()
		foundChat.ExtractedAt = &now
		foundChat.ExtractedCount += newTraitCount
	}
}

// ============================================================
// Shared trait extraction helpers (used by both agent and tasks packages)
// ============================================================

// buildTraitsLLMMessages builds the LLM message list from DB messages.
// If withTimestamps is true, prepends [timestamp] to each message.
func buildTraitsLLMMessages(title string, dbMessages []store.Message, lang string, withTimestamps bool) []llm.Message {
	systemContent := getTraitSystemPrompt(lang, title)

	llmMsgs := make([]llm.Message, 0, 1+len(dbMessages))
	llmMsgs = append(llmMsgs, llm.Message{
		Role:    llm.RoleSystem,
		Content: systemContent,
	})

	for _, m := range dbMessages {
		role := llm.RoleUser
		if m.Role == 1 {
			role = llm.RoleAssistant
		}

		content := m.Content
		if role == llm.RoleAssistant {
			runes := []rune(content)
			if len(runes) > 1024 {
				content = string(runes[:500]) + "\n...\n" + string(runes[len(runes)-500:])
			}
		}

		if withTimestamps {
			createAt := toolset.FormatTimeWithLocation(m.CreateAt)
			if createAt != "" {
				content = "[" + createAt + "] " + content
			}
		}

		llmMsgs = append(llmMsgs, llm.Message{
			Role:    role,
			Content: content,
		})
	}

	return llmMsgs
}

// callTraitsLLMWithTool sends messages to the LLM with the trip_traits tool and parses the result.
func callTraitsLLMWithTool(ctx context.Context, llmMsgs []llm.Message, lang string, client llm.Client, apiKey string) *TraitResult {
	tripTool := toolimp.NewTripTraitsTool(lang)

	reqBody := llm.ChatCompletionRequest{
		Model:    client.Model(),
		Messages: llmMsgs,
		Tools:    []llm.ToolDefinition{tripTool.GetDefinition()},
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	reqBody.ForceToolChoice(toolimp.TripTraitsToolName)

	resp, err := client.ChatWithOptions(ctx, reqBody, apiKey)
	if err != nil {
		return nil
	}

	result := &TraitResult{}

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
		for _, f := range traitsResult.Features {
			kws := make([]TraitKeywordItem, 0, len(f.Keywords))
			for _, kw := range f.Keywords {
				kws = append(kws, TraitKeywordItem{
					Type: kw.Type,
					Word: kw.Word,
				})
			}
			result.Features = append(result.Features, TraitFeature{
				CategoryID:   f.CategoryID,
				CategoryName: f.CategoryName,
				FeatureText:  f.FeatureText,
				Keywords:     kws,
				Confidence:   f.Confidence,
				HalfLife:     f.HalfLife,
				PrivacyLevel: f.PrivacyLevel,
			})
		}
	}

	return result
}

// CallTraitsLLMStandalone is an exported standalone function that builds LLM messages
// (without timestamps) and extracts traits. Usable from external packages like tasks.
func CallTraitsLLMStandalone(ctx context.Context, title string, dbMessages []store.Message, lang string, client llm.Client, apiKey string) *TraitResult {
	llmMsgs := buildTraitsLLMMessages(title, dbMessages, lang, false)
	if len(llmMsgs) <= 1 {
		return nil
	}
	return callTraitsLLMWithTool(ctx, llmMsgs, lang, client, apiKey)
}

// StoreTraitsStandalone is an exported standalone function that computes embeddings
// and persists traits, then atomically marks messages and session progress.
// Usable from external packages like tasks.
func StoreTraitsStandalone(ctx context.Context, features []TraitFeature, chatID int64, userID int64,
	upToMsgID int64, emb embedder.Embedder, apiKey string, dedupEnabled bool, dedupThreshold float64) (int, error) {

	insertions := make([]store.TraitInsertion, 0, len(features))
	for _, f := range features {
		if f.FeatureText == "" {
			continue
		}

		vector, err := emb.Embed(ctx, f.FeatureText, apiKey)
		if err != nil {
			return 0, fmt.Errorf("embed trait failed. %w", err)
		}

		// Deduplication: check if a similar trait already exists in the same chat.
		if dedupEnabled {
			existingTraits, err := theBrainStore.SearchByVectorInChat(userID, chatID, vector, 1)
			if err != nil {
				// Query failure should not block the insertion flow.
				logger.TheLogger().Warnf("dedup: SearchByVectorInChat failed for chat %d. %v", chatID, err)
			} else if len(existingTraits) > 0 && existingTraits[0].Score >= dedupThreshold {
				logger.TheLogger().Debugf("dedup: skip duplicate trait %q in chat %d (score=%.4f >= threshold=%.4f)",
					f.FeatureText, chatID, existingTraits[0].Score, dedupThreshold)
				continue
			}
		}

		trait := store.PersonalTrait{
			Trait:        f.FeatureText,
			Category:     f.CategoryID,
			Confidence:   f.Confidence,
			HalfLife:     HalfLifeToInt(f.HalfLife),
			PrivacyLevel: PrivacyLevelToInt(f.PrivacyLevel),
			ChatID:       chatID,
		}

		keywords := make([]store.TraitKeyword, 0, len(f.Keywords))
		for _, kw := range f.Keywords {
			if kw.Word == "" {
				continue
			}
			keywords = append(keywords, store.TraitKeyword{
				Word: kw.Word,
				Kind: KeywordTypeToInt(kw.Type),
			})
		}

		insertions = append(insertions, store.TraitInsertion{
			Trait:    trait,
			Vector:   vector,
			Keywords: keywords,
			UserID:   userID,
		})
	}

	// Single transaction: insert traits + mark messages + update session progress.
	return theBrainStore.AddTraits(ctx, chatID, upToMsgID, insertions)
}
