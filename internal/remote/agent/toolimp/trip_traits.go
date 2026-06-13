// Package toolimp provides ToolIMP implementations for the remote-server.
package toolimp

import (
	"encoding/json"
	"fmt"

	"BrainForever/infra/llm"
)

// ============================================================
// tripTraitsTool — implements llm.ToolIMP for trip_traits
//
// trip_traits is the tool definition for user personal trait extraction.
// It follows the 15-category schema defined in lang/remote/{lang}/system_prompt.toml.
// ============================================================

// TripTraitsToolName is the name of the user trait extraction tool.
const TripTraitsToolName = "trip_traits"

// TripTraitsParams matches the output format from lang/remote/{lang}/system_prompt.toml.
type TripTraitsParams struct {
	Features []TripTraitsFeature `json:"features"`
}

// TraitKeyword represents a single keyword associated with a trait feature.
// Type values (1-5): 1=时间, 2=地点, 3=人, 4=事物, 5=关系.
type TraitKeyword struct {
	Type int    `json:"type"`
	Word string `json:"word"`
}

// TripTraitsFeature represents a single extracted user trait.
type TripTraitsFeature struct {
	CategoryID   int            `json:"category_id"`
	CategoryName string         `json:"category_name"`
	FeatureText  string         `json:"feature_text"`
	Keywords     []TraitKeyword `json:"keywords"`
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
									"description": "简洁描述该特征的短句，尽量保留原意，可概括。注意：该值在 JSON 字符串中，如果内容包含双引号（\"），必须转义为 \\\"，以保证 JSON 语法正确。例如：'身高180cm', '喜欢好天气', '失眠严重'",
								},
								"keywords": map[string]any{
									"type":        "array",
									"description": "与该特征紧密相关的关键词列表。每个关键词包含 type（1-5）和 word（字符串）。type: 1=时间, 2=地点, 3=人, 4=事物, 5=关系。如果无法提取合理关键词可为空数组 []，但不能省略。",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"type": map[string]any{
												"type":        "number",
												"description": "关键词类型：1=时间, 2=地点, 3=人, 4=事物, 5=关系",
											},
											"word": map[string]any{
												"type":        "string",
												"description": "关键词文本，应尽量具体化、语义化，不必须直接出现在原文中",
											},
										},
										"required":             []string{"type", "word"},
										"additionalProperties": false,
									},
								},
							},
							"required":             []string{"category_id", "category_name", "feature_text", "keywords"},
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
// Primary decoding uses json.Unmarshal into TripTraitsParams (Fast Path).
// If the JSON is invalid because unescaped double quotes appear within string
// values, the fallback uses the json package's own encoding to re-escape them:
//
//  1. Scan the raw bytes to locate each feature JSON object via brace matching
//  2. For each feature, try standard json.Unmarshal
//  3. If feature_text has unescaped quotes, extract it as raw bytes, strip outer
//     JSON quotes, re-encode through json.Marshal (which properly escapes inner
//     double quotes), then decode through json.Unmarshal
//
// This approach relies on the json package for ALL string escaping, avoiding
// character-by-character sanitization.
func (t *TripTraitsTool) SetArgument(arguments string) error {
	// Fast path: standard json.Unmarshal (works when JSON is valid).
	if err := json.Unmarshal([]byte(arguments), &t.params); err == nil {
		return nil
	}

	// Fallback: extract features using byte-level scanning + json.Marshal re-encoding
	result, err := parseLenientJSON(arguments)
	if err != nil {
		return fmt.Errorf("parse arguments failed: %w", err)
	}

	t.params = TripTraitsParams{Features: result}
	return nil
}

// parseLenientJSON attempts to extract features from the arguments JSON text
// even when string values contain unescaped double quotes.
func parseLenientJSON(arguments string) ([]TripTraitsFeature, error) {
	data := []byte(arguments)

	// Find and extract each feature JSON object
	features, err := extractFeatureObjects(data)
	if err != nil {
		return nil, err
	}

	result := make([]TripTraitsFeature, 0, len(features))
	for _, feat := range features {
		f, err := decodeSingleFeature(feat)
		if err != nil {
			continue
		}
		result = append(result, f)
	}

	return result, nil
}

// extractFeatureObjects finds all brace-delimited objects within the "features" array.
func extractFeatureObjects(data []byte) ([][]byte, error) {
	featuresStart := findBytes(data, []byte(`"features"`))
	if featuresStart < 0 {
		return nil, fmt.Errorf("missing 'features' key")
	}

	afterKey := data[featuresStart+len(`"features"`):]
	arrStart := indexOfByte(afterKey, '[')
	if arrStart < 0 {
		return nil, fmt.Errorf("missing 'features' array")
	}
	afterBracket := afterKey[arrStart:]

	var objects [][]byte
	braceDepth := 0
	objStart := -1

	for i := 0; i < len(afterBracket); i++ {
		ch := afterBracket[i]
		switch ch {
		case '{':
			if braceDepth == 0 {
				objStart = i
			}
			braceDepth++
		case '}':
			braceDepth--
			if braceDepth == 0 && objStart >= 0 {
				objects = append(objects, afterBracket[objStart:i+1])
				objStart = -1
			}
		}
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("no feature objects found")
	}

	return objects, nil
}

// findBytes finds the first occurrence of needle in data.
func findBytes(data, needle []byte) int {
	for i := 0; i <= len(data)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if data[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// indexOfByte finds the first occurrence of b in data.
func indexOfByte(data []byte, b byte) int {
	for i, ch := range data {
		if ch == b {
			return i
		}
	}
	return -1
}

// decodeSingleFeature decodes one feature JSON object, handling unescaped
// double quotes within the feature_text value by re-encoding through json.Marshal.
func decodeSingleFeature(data []byte) (TripTraitsFeature, error) {
	// First, try standard unmarshal
	var f TripTraitsFeature
	if err := json.Unmarshal(data, &f); err == nil {
		return f, nil
	}

	// Fallback: manually extract fields
	f = TripTraitsFeature{}
	f.CategoryID = extractIntField(data, "category_id")
	f.CategoryName = extractStringFieldDirect(data, "category_name")
	f.FeatureText = extractStringFieldReEscaped(data, "feature_text")
	f.Keywords = extractKeywordsField(data)
	return f, nil
}

// extractKeywordsField extracts the "keywords" array from a feature JSON object.
func extractKeywordsField(data []byte) []TraitKeyword {
	raw := extractRawFieldValue(data, "keywords")
	if len(raw) == 0 {
		return nil
	}

	var keywords []TraitKeyword
	if err := json.Unmarshal(raw, &keywords); err == nil {
		return keywords
	}

	// Fallback: try lenient extraction if standard unmarshal fails
	// Scan for objects within the array
	keywords = make([]TraitKeyword, 0)
	depth := 0
	objStart := -1
	inStr := false
	esc := false

	for i := 0; i < len(raw); i++ {
		if esc {
			esc = false
			continue
		}
		if raw[i] == '\\' {
			esc = true
			continue
		}
		if raw[i] == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}

		switch raw[i] {
		case '{':
			if depth == 0 {
				objStart = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && objStart >= 0 {
				objBytes := raw[objStart : i+1]
				var kw TraitKeyword
				if err := json.Unmarshal(objBytes, &kw); err == nil {
					keywords = append(keywords, kw)
				} else {
					// Individual keyword fallback
					kw.Type = extractIntField(objBytes, "type")
					kw.Word = extractStringFieldDirect(objBytes, "word")
					keywords = append(keywords, kw)
				}
				objStart = -1
			}
		}
	}

	return keywords
}

// extractIntField extracts an integer field from a JSON object byte slice.
func extractIntField(data []byte, field string) int {
	val := extractRawFieldValue(data, field)
	if len(val) == 0 {
		return 0
	}
	var n int
	json.Unmarshal(val, &n)
	return n
}

// extractStringFieldDirect extracts a string field using standard json.Unmarshal.
func extractStringFieldDirect(data []byte, field string) string {
	val := extractRawFieldValue(data, field)
	if len(val) == 0 {
		return ""
	}
	var s string
	json.Unmarshal(val, &s)
	return s
}

// extractStringFieldReEscaped extracts a string field's content even when it
// contains unescaped double quotes, by re-encoding through json.Marshal.
//
// How it works:
//  1. The raw field value is a JSON string like: "偏好用"揭穿"的方式"
//     (with inner double quotes not escaped)
//  2. We strip the outer quotes, getting: 偏好用"揭穿"的方式
//  3. We call json.Marshal(inner) to produce a properly escaped JSON string
//     (this escapes the inner " to \")
//  4. json.Unmarshal the result to get the clean Go string
func extractStringFieldReEscaped(data []byte, field string) string {
	val := extractRawFieldValue(data, field)
	if len(val) == 0 {
		return ""
	}

	// Try standard unmarshal first
	var s string
	if err := json.Unmarshal(val, &s); err == nil {
		return s
	}

	// Standard unmarshal failed (likely unescaped quotes within the value).
	// Use json.Marshal to properly re-escape the content.
	raw := string(val)
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return ""
	}
	inner := raw[1 : len(raw)-1]

	// json.Marshal(inner) produces a properly escaped JSON string value.
	// For example: inner = `偏好用"揭穿"的方式`
	// json.Marshal → `"偏好用\"揭穿\"的方式"` (properly escaped)
	escaped, err := json.Marshal(inner)
	if err != nil {
		return ""
	}

	// Now unmarshal the properly escaped JSON string
	if err := json.Unmarshal(escaped, &s); err != nil {
		return ""
	}
	return s
}

// extractRawFieldValue finds the raw JSON value for a field in a JSON object.
// It scans for `"fieldName":` and extracts the value bytes using a
// depth-aware approach for primitive values.
func extractRawFieldValue(data []byte, field string) []byte {
	quoted := []byte(`"` + field + `":`)
	pos := findBytes(data, quoted)
	if pos < 0 {
		return nil
	}

	valStart := pos + len(quoted)
	if valStart >= len(data) {
		return nil
	}

	rest := data[valStart:]

	// Determine value type by first character
	switch {
	case rest[0] == '"':
		// String value — scan to ending quote, accounting for escapes
		inEscape := false
		for i := 1; i < len(rest); i++ {
			if inEscape {
				inEscape = false
				continue
			}
			if rest[i] == '\\' {
				inEscape = true
				continue
			}
			if rest[i] == '"' {
				return rest[:i+1]
			}
		}
		return rest

	case rest[0] == '{' || rest[0] == '[':
		// Object or array — use brace/bracket counting
		open := rest[0]
		close := byte('}')
		if open == '[' {
			close = ']'
		}
		depth := 0
		inStr := false
		esc := false
		for i := 0; i < len(rest); i++ {
			if esc {
				esc = false
				continue
			}
			if rest[i] == '\\' {
				esc = true
				continue
			}
			if rest[i] == '"' {
				inStr = !inStr
				continue
			}
			if !inStr {
				switch rest[i] {
				case open:
					depth++
				case close:
					depth--
					if depth == 0 {
						return rest[:i+1]
					}
				}
			}
		}
		return rest

	default:
		// Number, boolean, null — scan until non-value character
		for i := 0; i < len(rest); i++ {
			ch := rest[i]
			if ch == ',' || ch == '}' || ch == ']' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
				return rest[:i]
			}
		}
		return rest
	}
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
