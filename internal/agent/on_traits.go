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
}

type traitsKeyword struct {
	Type string `json:"type"`
	Word string `json:"word"`
}

type traitsResponse struct {
	Features []traitsFeature `json:"features,omitempty"`
	Usage    interface{}     `json:"usage,omitempty"`
	Error    string          `json:"error,omitempty"`

	ExtractedAt    *string `json:"extracted_at,omitempty"`
	ExtractedCount int     `json:"extracted_count,omitempty"`
}

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
		return 2
	}
}

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
		return 4
	}
}

// OnExtractTraits handles POST /api/chat/traits
func (h *ChatAgent) OnExtractTraits(w http.ResponseWriter, r *http.Request) {
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))

	var req traitsFrontendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_failed_to_parse_request", map[string]any{"Error": err.Error()}), http.StatusBadRequest)
		return
	}

	if req.SN == "" {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_parameter_required", map[string]any{"Param": "sn"}), http.StatusBadRequest)
		return
	}

	sessionID := h.resolveSessionID(w, r)
	sess := h.sessionManager.GetOrCreate(sessionID)

	foundChat := sess.FindChatBySN(req.SN)
	if foundChat == nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_chat_not_found"), http.StatusNotFound)
		return
	}

	chatStore, cerr := h.openChatDB(sess)
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

	remoteResp, err := h.callTraitsLLM(r.Context(), req.SN, foundChat.Title, dbMessages, lang, sess)
	if err != nil {
		toolset.WriteJSONError(w, i18n.TL(lang, "api_error_internal"), http.StatusInternalServerError)
		return
	}

	lastMsgID := dbMessages[len(dbMessages)-1].ID

	if len(remoteResp.Features) > 0 {
		storedCount, _ := h.storeTraitsInSession(r.Context(), sess, remoteResp.Features, foundChat.SN)
		if storedCount > 0 {
			chatStore.UpdateMessagesExtracted(foundChat.ID, lastMsgID, true)
			updateExtractionProgress(foundChat, chatStore, storedCount)
		} else {
			updateExtractionProgress(foundChat, chatStore, 0)
		}
	} else {
		updateExtractionProgress(foundChat, chatStore, 0)
	}

	if foundChat.ExtractedAt != nil {
		extractedAtStr := foundChat.ExtractedAt.Format(time.RFC3339)
		remoteResp.ExtractedAt = &extractedAtStr
		remoteResp.ExtractedCount = foundChat.ExtractedCount
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(remoteResp)
}

func (h *ChatAgent) callTraitsLLM(ctx context.Context, sn, title string, dbMessages []store.Message, lang string, sess *session.Session) (*traitsResponse, error) {
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

	tripTool := toolimp.NewTripTraitsTool(lang)

	client := sessionLLMClient(sess)
	apiSetting := sessionLLMApiSetting(sess)

	reqBody := llm.ChatCompletionRequest{
		Model:    client.Model(),
		Messages: llmMsgs,
		Tools:    []llm.ToolDefinition{tripTool.GetDefinition()},
		Thinking: &llm.ThinkingConfig{Type: "disabled"},
	}
	reqBody.ForceToolChoice(toolimp.TripTraitsToolName)

	resp, err := client.ChatWithOptions(ctx, reqBody, apiSetting.ApiKey)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

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

func getTraitSystemPrompt(lang string, chatTitle string) string {
	return i18n.SystemPrompt.TL(lang, "trip_trait", map[string]interface{}{
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

func (h *ChatAgent) storeTraitsInSession(ctx context.Context, sess *session.Session, features []traitsFeature, chatSN string) (int, error) {
	emb := sessionEmbedder(sess)
	apiSetting := sessionEmbedderApiSetting(sess)

	vs, err := h.openBrainDB(sess)
	if err != nil {
		return 0, fmt.Errorf("open traits store: %w", err)
	}
	defer h.closeBrainDB(vs)

	stored := 0
	for _, f := range features {
		if f.FeatureText == "" {
			continue
		}

		vector, err := emb.Embed(ctx, f.FeatureText, apiSetting.ApiKey)
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
