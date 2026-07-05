package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
	"BrainForever/toolset"
)

// ============================================================
// Trait extraction handler -POST /api/chat/traits
//
// Flow:
//  1. Frontend sends POST /api/chat/traits with {"sn": "xxx"}
//  2. Local-server reads chat messages from local DB
//  3. Local-server calls DeepSeek API directly (non-streaming)
//     with the trip_traits tool to extract personal traits
//  4. Local-server embeds each trait via the embedder and stores
//     traits + keywords into the user-specific traits DB
//  5. Local-server returns the result to the frontend
//
// Traits DB naming:
//   - Anonymous: localdb/anonymous.brain.db
//   - Logged-in: localdb/{userNo}.brain.db
// ============================================================

// traitsFrontendRequest is the request from the frontend to local-server.
type traitsFrontendRequest struct {
	SN string `json:"sn"`
}

// traitsMsg is a single message for LLM context (kept for message conversion).
type traitsMsg struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	CreateAt string `json:"create_at"`
}

// traitsFeature is a single extracted feature returned to the frontend.
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

// traitsResponse is the response returned to the frontend.
type traitsResponse struct {
	Features []traitsFeature `json:"features,omitempty"`
	Usage    interface{}     `json:"usage,omitempty"`
	Error    string          `json:"error,omitempty"`

	// Extraction state
	ExtractedAt    *string `json:"extracted_at,omitempty"`
	ExtractedCount int     `json:"extracted_count,omitempty"`
}

// halfLifeToInt converts the half-life string from the LLM to an integer.
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

// OnExtractTraits handles POST /api/chat/traits -- accepts a chat SN,
// reads the chat messages from the local database, then calls the LLM
// directly with the trip_traits tool, embeds and stores the results,
// and returns the features to the frontend.
func (h *ChatAgent) OnExtractTraits(w http.ResponseWriter, r *http.Request) {
	// ----------------------------------------------------------
	// 0. Determine language upfront
	// ----------------------------------------------------------
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))

	// ----------------------------------------------------------
	// 1. Parse request body
	// ----------------------------------------------------------
	var req traitsFrontendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.SN == "" {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	// ----------------------------------------------------------
	// 2. Resolve session and find the chat
	// ----------------------------------------------------------
	sessionID := h.resolveSessionID(w, r)
	session := h.sessionManager.GetOrCreate(sessionID)

	foundChat := session.findChatBySN(req.SN)
	if foundChat == nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_chat_not_found"), http.StatusNotFound)
		return
	}

	// ----------------------------------------------------------
	// 3. Read un-extracted messages from database
	// ----------------------------------------------------------
	chatStore, cerr := h.openChatDB(session)
	if cerr != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_failed_to_open_chat_store"), http.StatusInternalServerError)
		return
	}
	defer h.closeChatDB(chatStore)

	dbMessages, err := chatStore.ListUnExtractMessages(foundChat.ID)
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_failed_to_list_messages", map[string]any{"Error": err.Error()}), http.StatusInternalServerError)
		return
	}

	if len(dbMessages) == 0 {
		handleNoNewMessages(w, foundChat, chatStore)
		return
	}

	// ----------------------------------------------------------
	// 4. Convert messages and call LLM directly
	// ----------------------------------------------------------
	remoteResp, err := h.callTraitsLLM(r.Context(), req.SN, foundChat.Title, dbMessages, lang)
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_internal"), http.StatusInternalServerError)
		return
	}

	// ----------------------------------------------------------
	// 5. Embed each trait and store into user-specific traits DB
	// ----------------------------------------------------------
	lastMsgID := dbMessages[len(dbMessages)-1].ID

	if len(remoteResp.Features) > 0 {
		storedCount, _ := h.storeTraitsInSession(r.Context(), session, remoteResp.Features, foundChat.SN)
		if storedCount > 0 {
			chatStore.UpdateMessagesExtracted(foundChat.ID, lastMsgID, true)
			updateExtractionProgress(foundChat, chatStore, storedCount)
		} else {
			updateExtractionProgress(foundChat, chatStore, 0)
		}
	} else {
		updateExtractionProgress(foundChat, chatStore, 0)
	}

	// ----------------------------------------------------------
	// 6. Populate extraction state and return
	// ----------------------------------------------------------
	if foundChat.ExtractedAt != nil {
		extractedAtStr := foundChat.ExtractedAt.Format(time.RFC3339)
		remoteResp.ExtractedAt = &extractedAtStr
		remoteResp.ExtractedCount = foundChat.ExtractedCount
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(remoteResp)
}

// ============================================================
// Direct LLM call for trait extraction
// ============================================================

// callTraitsLLM builds the LLM request with the trip_traits tool
// and returns the parsed extraction result.
func (h *ChatAgent) callTraitsLLM(ctx context.Context, sn, title string, dbMessages []store.Message, lang string) (*traitsResponse, error) {
	// 1. Build system prompt with i18n
	systemContent := getTraitSystemPrompt(lang, title)

	// 2. Build LLM messages
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

		// Add timestamp prefix
		createAt := toolset.FormatTimeWithLocation(m.CreateAt)
		if createAt != "" {
			content = "[" + createAt + "] " + content
		}

		llmMsgs = append(llmMsgs, llm.Message{
			Role:    role,
			Content: content,
		})
	}

	if len(llmMsgs) <= 1 {
		return nil, fmt.Errorf("no valid messages after conversion")
	}

	// 3. Create the trip_traits tool
	tripTool := toolimp.NewTripTraitsTool(lang)

	// 4. Build LLM request with ForceToolChoice
	reqBody := llm.ChatCompletionRequest{
		Model:    h.charLLMClient.Model(),
		Messages: llmMsgs,
		Tools:    []llm.ToolDefinition{tripTool.GetDefinition()},
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	reqBody.ForceToolChoice(toolimp.TripTraitsToolName)

	// 5. Call LLM (non-streaming)
	resp, err := h.charLLMClient.ChatWithOptions(ctx, reqBody)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// 6. Parse tool calls from the response
	result := &traitsResponse{}

	if resp.Usage != nil && resp.Usage.TotalTokens > 0 {
		result.Usage = resp.Usage
	}

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
		// Convert from toolimp types to local response types
		for _, f := range traitsResult.Features {
			kws := make([]traitsKeyword, 0, len(f.Keywords))
			for _, kw := range f.Keywords {
				kws = append(kws, traitsKeyword{
					Type: kw.Type,
					Word: kw.Word,
				})
			}
			result.Features = append(result.Features, traitsFeature{
				CategoryID:   f.CategoryID,
				CategoryName: f.CategoryName,
				FeatureText:  f.FeatureText,
				Keywords:     kws,
				Confidence:   f.Confidence,
				HalfLife:     f.HalfLife,
			})
		}
	} else if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
		result.Error = "LLM returned text instead of tool call"
	}

	return result, nil
}

// ============================================================
// System prompt helper
// ============================================================

// getTraitSystemPrompt returns the localized system prompt for trip_traits extraction.
// The prompt content is stored in lang/{lang}/system_prompt.toml under key "trip_trait".
func getTraitSystemPrompt(lang string, chatTitle string) string {
	return i18n.SystemPrompt.TL(lang, "trip_trait", map[string]interface{}{
		"CurrentLocalTime": time.Now().In(time.Local).Format("2006-01-02 15:04:05 (MST)"),
		"ChatTitle":        chatTitle,
	})
}

// ============================================================
// Helper: message conversion (kept from original)
// ============================================================

// dbMessagesToTraitsMsgs converts DB messages to the traits message format.
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

// ============================================================
// Helper: store traits (unchanged from original)
// ============================================================

// storeTraitsInSession embeds each trait feature and stores it along with keywords
// into the session's per-user traits database.
func (h *ChatAgent) storeTraitsInSession(ctx context.Context, session *session, features []traitsFeature, chatSN string) (int, error) {
	emb := h.embedder

	vs, err := h.openBrainDB(session)
	if err != nil {
		return 0, fmt.Errorf("open traits store: %w", err)
	}
	defer h.closeBrainDB(vs)

	stored := 0
	for _, f := range features {
		if f.FeatureText == "" {
			continue
		}

		vector, err := emb.Embed(ctx, f.FeatureText)
		if err != nil {
			continue
		}

		trait := &store.PersonalTrait{
			Trait:      f.FeatureText,
			Category:   f.CategoryID,
			Confidence: f.Confidence,
			HalfLife:   halfLifeToInt(f.HalfLife),
			ChatSN:     chatSN,
		}

		traitID, err := vs.AddTrait(ctx, trait, vector)
		if err != nil {
			continue
		}

		for _, kw := range f.Keywords {
			if kw.Word == "" {
				continue
			}
			keyword := &store.TraitKeyword{
				Word:    kw.Word,
				Kind:    keywordTypeToInt(kw.Type),
				TraitID: traitID,
			}
			vs.AddKeyword(keyword)
		}

		stored++
	}

	return stored, nil
}

// ============================================================
// Helpers (unchanged from original)
// ============================================================

func handleNoNewMessages(w http.ResponseWriter, foundChat *store.Chat, chatsStore *store.ChatStore) {
	resp := traitsResponse{
		Features: []traitsFeature{},
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
