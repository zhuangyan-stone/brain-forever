// Package toolimp provides ToolIMP implementations for the remote-server.
package toolimp

import (
	"encoding/json"

	"BrainForever/infra/llm"
)

// ============================================================
// tripTraitsTool — implements llm.ToolIMP for trip_traits
//
// trip_traits is the tool definition for user personal trait extraction.
// It follows the 15-category schema defined in doc/特性提取提示词v1.md.
// ============================================================

// TripTraitsToolName is the name of the user trait extraction tool.
const TripTraitsToolName = "trip_traits"

// TripTraitsParams matches the output format from doc/特性提取提示词v1.md.
type TripTraitsParams struct {
	Features []TripTraitsFeature `json:"features"`
}

// TripTraitsFeature represents a single extracted user trait.
type TripTraitsFeature struct {
	CategoryID   int    `json:"category_id"`
	CategoryName string `json:"category_name"`
	FeatureText  string `json:"feature_text"`
}

// TripTraitsTool implements llm.ToolIMP for the trip_traits tool.
type TripTraitsTool struct {
	def    llm.ToolDefinition
	params TripTraitsParams
}

// Compile-time interface check.
var _ llm.ToolIMP = (*TripTraitsTool)(nil)

// NewTripTraitsTool creates a TripTraitsTool with a strict schema definition.
func NewTripTraitsTool() *TripTraitsTool {
	strict := true

	def := llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TripTraitsToolName,
			Strict:      &strict,
			Description: "从用户与AI的对话中，识别并提取与用户本人相关的稳定或临时特征。调用此工具以输出特征提取的结构化结果。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"features": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"category_id": map[string]any{
									"type":        "number",
									"description": "特征类别编号（0=其他,1=人口学特性,2=外部客观事实,3=文化修为,4=兴趣爱好,5=能力技能,6=偏好/癖好,7=行为习惯,8=健康与疾病,9=情况和状态,10=人格/性格特征,11=价值观与信仰,12=社交关系,13=人生经历,14=目标与动机）",
								},
								"category_name": map[string]any{
									"type":        "string",
									"description": "中文类别名（与category_id对应）：其他,人口学特性,外部客观事实,文化修为,兴趣爱好,能力技能,偏好/癖好,行为习惯,健康与疾病,情况和状态,人格/性格特征,价值观与信仰,社交关系,人生经历,目标与动机",
								},
								"feature_text": map[string]any{
									"type":        "string",
									"description": "简洁描述该特征的短句，尽量保留原意，可概括。例如：'身高180cm', '喜欢好天气', '失眠严重'",
								},
							},
							"required":             []string{"category_id", "category_name", "feature_text"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"features"},
				"additionalProperties": false,
			},
		},
	}
	return &TripTraitsTool{def: def}
}

// GetName returns the tool name.
func (t *TripTraitsTool) GetName() string { return TripTraitsToolName }

// GetDefinition returns the tool definition for the LLM.
func (t *TripTraitsTool) GetDefinition() llm.ToolDefinition { return t.def }

// SetArgument parses and stores the JSON arguments from the LLM tool call.
func (t *TripTraitsTool) SetArgument(arguments string) error {
	return json.Unmarshal([]byte(arguments), &t.params)
}

// GetPendingText returns a human-readable description shown while the tool is pending.
func (t *TripTraitsTool) GetPendingText() string { return "正在提取用户特征..." }

// Execute returns the extracted traits as a JSON string (for the LLM to consume).
func (t *TripTraitsTool) Execute() (string, error) {
	result, _ := json.Marshal(t.params)
	return string(result), nil
}

// GetTraitsResult returns the parsed traits result for the caller.
func (t *TripTraitsTool) GetTraitsResult() TripTraitsParams {
	return t.params
}
