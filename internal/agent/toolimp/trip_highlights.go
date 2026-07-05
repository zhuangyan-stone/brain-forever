// Package toolimp provides ToolIMP implementations.
package toolimp

import (
	"encoding/json"
	"fmt"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
)

// ============================================================
// trip_highlights tool — extract core traits & key highlights
// from a completed user portrait text.
//
// This tool is used in Step 2 of portrait generation:
// after the LLM streams the full portrait text, a second
// non-streaming LLM call (with ForceToolChoice) invokes this
// tool to extract structured metadata.
// ============================================================

// TripHighlightsToolName is the name of the portrait highlights extraction tool.
const TripHighlightsToolName = "trip_highlights"

// TripHighlightsParams matches the expected output format.
type TripHighlightsParams struct {
	CoreTraits    []string `json:"core_traits"`
	KeyHighlights []string `json:"key_highlights"`
}

// TripHighlightsTool implements llm.ToolIMP for the trip_highlights tool.
type TripHighlightsTool struct {
	lang   string
	def    llm.ToolDefinition
	params TripHighlightsParams
}

// Compile-time interface check.
var _ llm.ToolIMP = (*TripHighlightsTool)(nil)

// tripHighlightsToolDefinition builds the ToolDefinition with localized descriptions.
func tripHighlightsToolDefinition(lang string) llm.ToolDefinition {
	strict := true

	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TripHighlightsToolName,
			Strict:      &strict,
			Description: i18n.Tools.TL(lang, TripHighlightsToolName, "description"),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"core_traits": map[string]any{
						"type":        "array",
						"description": i18n.Tools.TL(lang, TripHighlightsToolName, "param_core_traits_desc"),
						"items": map[string]any{
							"type": "string",
						},
					},
					"key_highlights": map[string]any{
						"type":        "array",
						"description": i18n.Tools.TL(lang, TripHighlightsToolName, "param_key_highlights_desc"),
						"items": map[string]any{
							"type": "string",
						},
					},
				},
				"required":             []string{"core_traits", "key_highlights"},
				"additionalProperties": false,
			},
		},
	}
}

// NewTripHighlightsTool creates a TripHighlightsTool with localized descriptions.
func NewTripHighlightsTool(lang string) *TripHighlightsTool {
	return &TripHighlightsTool{
		lang: lang,
		def:  tripHighlightsToolDefinition(lang),
	}
}

// GetName returns the tool name.
func (t *TripHighlightsTool) GetName() string { return TripHighlightsToolName }

// GetDefinition returns the tool definition for the LLM.
func (t *TripHighlightsTool) GetDefinition() llm.ToolDefinition { return t.def }

// SetArgument parses and stores the JSON arguments from the LLM tool call.
func (t *TripHighlightsTool) SetArgument(arguments string) error {
	return json.Unmarshal([]byte(arguments), &t.params)
}

// GetPendingText returns a human-readable description shown while the tool is pending.
func (t *TripHighlightsTool) GetPendingText() string {
	return i18n.Tools.TL(t.lang, TripHighlightsToolName, "pending")
}

// Execute returns the extracted params as a JSON string (for the LLM to consume).
func (t *TripHighlightsTool) Execute() (string, error) {
	result, err := json.Marshal(t.params)
	if err != nil {
		return "", fmt.Errorf("marshal trip_highlights result failed: %w", err)
	}
	return string(result), nil
}

// GetResult returns the parsed extraction result.
func (t *TripHighlightsTool) GetResult() TripHighlightsParams {
	return t.params
}
