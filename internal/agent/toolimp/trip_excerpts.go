package toolimp

import (
	"encoding/json"
	"fmt"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
)

// ============================================================
// trip_excerpts -implements llm.ToolIMP for user quote excerpt output
//
// trip_excerpts is the tool definition for user quote excerpt extraction.
// It follows the schema defined in lang/{lang}/system_prompt.toml [excerpt].
// ============================================================

// TripExcerptsToolName is the name of the excerpt extraction tool.
const TripExcerptsToolName = "trip_excerpts"

// TripExcerptsParams matches the output format from [excerpt] system prompt.
type TripExcerptsParams struct {
	Excerpts []TripExcerptsItem `json:"excerpts"`
}

// TripExcerptsItem represents a single excerpt entry.
type TripExcerptsItem struct {
	ExcerptText    string   `json:"excerpt_text"`
	ValueTypes     []string `json:"value_types"`
	ContextSummary string   `json:"context_summary"`
	Reason         string   `json:"reason"`
	MsgID          int64    `json:"msg_id"`
}

// TripExcerptsTool implements llm.ToolIMP for the trip_excerpts tool.
type TripExcerptsTool struct {
	lang   string
	def    llm.ToolDefinition
	params TripExcerptsParams
}

// Compile-time interface check.
var _ llm.ToolIMP = (*TripExcerptsTool)(nil)

// tripExcerptsToolDefinition builds a ToolDefinition with descriptions localized
// to the given language via i18n.
func tripExcerptsToolDefinition(lang string) llm.ToolDefinition {
	strict := true

	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TripExcerptsToolName,
			Strict:      &strict,
			Description: i18n.Tools.TL(lang, TripExcerptsToolName, "description"),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"excerpts": map[string]any{
						"type":        "array",
						"description": i18n.Tools.TL(lang, TripExcerptsToolName, "param_excerpts_desc"),
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"excerpt_text": map[string]any{
									"type":        "string",
									"description": i18n.Tools.TL(lang, TripExcerptsToolName, "param_excerpt_text_desc"),
								},
								"value_types": map[string]any{
									"type":        "array",
									"description": i18n.Tools.TL(lang, TripExcerptsToolName, "param_value_types_desc"),
									"items": map[string]any{
										"type": "string",
										"enum": []string{
											"insight", "humor", "vent", "methodology", "rule",
											"confession", "nostalgia", "regret", "self_discovery",
											"conviction", "touching", "deed", "privacy", "literary",
										},
									},
								},
								"context_summary": map[string]any{
									"type":        "string",
									"description": i18n.Tools.TL(lang, TripExcerptsToolName, "param_context_summary_desc"),
								},
								"reason": map[string]any{
									"type":        "string",
									"description": i18n.Tools.TL(lang, TripExcerptsToolName, "param_reason_desc"),
								},
								"msg_id": map[string]any{
									"type":        "number",
									"description": i18n.Tools.TL(lang, TripExcerptsToolName, "param_msg_id_desc"),
								},
							},
							"required":             []string{"excerpt_text", "value_types", "context_summary", "reason", "msg_id"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"excerpts"},
				"additionalProperties": false,
			},
		},
	}
}

// NewTripExcerptsTool creates a TripExcerptsTool with localized descriptions.
func NewTripExcerptsTool(lang string) *TripExcerptsTool {
	return &TripExcerptsTool{
		lang: lang,
		def:  tripExcerptsToolDefinition(lang),
	}
}

// GetName returns the tool name.
func (t *TripExcerptsTool) GetName() string { return TripExcerptsToolName }

// GetDefinition returns the tool definition for the LLM.
func (t *TripExcerptsTool) GetDefinition() llm.ToolDefinition { return t.def }

// SetArgument parses and stores the JSON arguments from the LLM tool call.
func (t *TripExcerptsTool) SetArgument(arguments string) error {
	if err := json.Unmarshal([]byte(arguments), &t.params); err != nil {
		return fmt.Errorf("parse trip_excerpts arguments failed. %w", err)
	}
	return nil
}

// GetPendingText returns a human-readable description shown while the tool is pending.
func (t *TripExcerptsTool) GetPendingText() string {
	return i18n.Tools.TL(t.lang, TripExcerptsToolName, "pending")
}

// Execute returns the parsed excerpts as a JSON string (for the LLM to consume).
func (t *TripExcerptsTool) Execute() (string, error) {
	result, _ := json.Marshal(t.params)
	return string(result), nil
}

// GetExcerptsResult returns the parsed excerpts result for the caller.
func (t *TripExcerptsTool) GetExcerptsResult() TripExcerptsParams {
	return t.params
}
