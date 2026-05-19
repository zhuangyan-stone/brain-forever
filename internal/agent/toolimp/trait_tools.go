package toolimp

import (
	"encoding/json"
	"fmt"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
)

// ============================================================
// traits_extracted — called after AI completes trait extraction
// ============================================================

// TraitsExtractedToolName is the name of the tool used when personal traits have been extracted.
const TraitsExtractedToolName = "traits_extracted"

// TraitsExtractedResult represents a single extracted trait item.
type TraitsExtractedResult struct {
	Topic           string
	InferenceMethod string
	Nature          string
	Conclusion      string
	Scenario        string
	Domain          string
	Category        string
	Source          string
	Confidence      float64
	HalfLife        string
}

// traitsExtractedArguments parses the JSON arguments for traits_extracted.
func traitsExtractedArguments(arguments string) ([]TraitsExtractedResult, error) {
	var params struct {
		Traits []struct {
			Topic           string  `json:"topic"`
			InferenceMethod string  `json:"inference_method"`
			Nature          string  `json:"nature"`
			Conclusion      string  `json:"conclusion"`
			Scenario        string  `json:"scenario"`
			Domain          string  `json:"domain"`
			Category        string  `json:"category"`
			Source          string  `json:"source"`
			Confidence      float64 `json:"confidence"`
			HalfLife        string  `json:"half_life"`
		} `json:"traits"`
	}
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return nil, fmt.Errorf("json unmarshal fail. %w", err)
	}

	results := make([]TraitsExtractedResult, 0, len(params.Traits))
	for _, t := range params.Traits {
		results = append(results, TraitsExtractedResult{
			Topic:           t.Topic,
			InferenceMethod: t.InferenceMethod,
			Nature:          t.Nature,
			Conclusion:      t.Conclusion,
			Scenario:        t.Scenario,
			Domain:          t.Domain,
			Category:        t.Category,
			Source:          t.Source,
			Confidence:      t.Confidence,
			HalfLife:        t.HalfLife,
		})
	}
	return results, nil
}

// TraitsExtractedToolDefinition returns the ToolDefinition for the traits_extracted tool.
func TraitsExtractedToolDefinition(lang string) llm.ToolDefinition {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"traits": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"topic": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_topic_desc"),
						},
						"inference_method": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_inference_method_desc"),
							"enum":        []string{"explicit", "implicit"},
						},
						"nature": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_nature_desc"),
							"enum":        []string{"objective", "subjectivity"},
						},
						"conclusion": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_conclusion_desc"),
						},
						"scenario": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_scenario_desc"),
							"enum":        []string{"casual", "work", "study", "life", "health", "other"},
						},
						"domain": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_domain_desc"),
						},
						"category": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_category_desc"),
							"enum":        []string{"demographic", "psychological", "predilection", "avocation", "capability", "habit", "state", "social", "other"},
						},
						"source": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_source_desc"),
						},
						"confidence": map[string]any{
							"type":        "number",
							"description": i18n.TL(lang, "traits_extracted_param_confidence_desc"),
							"minimum":     0.1,
							"maximum":     1.0,
						},
						"half_life": map[string]any{
							"type":        "string",
							"description": i18n.TL(lang, "traits_extracted_param_half_life_desc"),
							"enum":        []string{"short", "medium", "long"},
						},
					},
					"required": []string{
						"topic",
						"inference_method",
						"nature",
						"conclusion",
						"scenario",
						"domain",
						"category",
						"source",
						"confidence",
						"half_life",
					},
					"additionalProperties": false,
				},
				"description": i18n.TL(lang, "traits_extracted_param_traits_desc"),
			},
		},
		"required":             []string{"traits"},
		"additionalProperties": false,
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal traits_extracted schema: %v", err))
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(schemaBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to parse traits_extracted schema: %v", err))
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TraitsExtractedToolName,
			Description: i18n.TL(lang, "traits_extracted_tool_description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}

// TraitsExtractedToolImp implements llm.ToolIMP for the traits_extracted tool.
type TraitsExtractedToolImp struct {
	def    llm.ToolDefinition
	lang   string
	traits []TraitsExtractedResult
}

// MakeTraitsExtractedTool creates a TraitsExtractedToolImp.
func MakeTraitsExtractedTool(lang string) *TraitsExtractedToolImp {
	return &TraitsExtractedToolImp{
		def:  TraitsExtractedToolDefinition(lang),
		lang: lang,
	}
}

var _ llm.ToolIMP = (*TraitsExtractedToolImp)(nil)

func (imp *TraitsExtractedToolImp) GetName() string {
	return TraitsExtractedToolName
}

func (imp *TraitsExtractedToolImp) GetDefinition() llm.ToolDefinition {
	return imp.def
}

func (imp *TraitsExtractedToolImp) SetArgument(arguments string) error {
	traits, err := traitsExtractedArguments(arguments)
	if err != nil {
		return err
	}
	imp.traits = traits
	return nil
}

func (imp *TraitsExtractedToolImp) GetPendingText() string {
	return i18n.TL(imp.lang, "traits_extracted_tool_pending")
}

func (imp *TraitsExtractedToolImp) GetTraits() []TraitsExtractedResult {
	return imp.traits
}

func (imp *TraitsExtractedToolImp) Execute() (string, error) {
	if len(imp.traits) == 0 {
		return `{"status":"ok","count":0}`, nil
	}

	result := map[string]any{
		"status": "ok",
		"count":  len(imp.traits),
	}
	resultBytes, _ := json.Marshal(result)
	return string(resultBytes), nil
}

// ============================================================
// topic_shift_detected — called when AI detects a user topic shift
// ============================================================

// TopicShiftDetectedToolName is the name of the tool used when the AI detects a user topic shift.
const TopicShiftDetectedToolName = "topic_shift_detected"

// TopicShiftDetectedResult stores the parsed topic shift detected data.
type TopicShiftDetectedResult struct {
	Topics      []string
	Recommended string
	Candidates  []string
}

// topicShiftDetectedArguments parses the JSON arguments for topic_shift_detected.
func topicShiftDetectedArguments(arguments string) (*TopicShiftDetectedResult, error) {
	var params struct {
		Topics      []string `json:"topics,omitempty"`
		Recommended string   `json:"recommended,omitempty"`
		Candidates  []string `json:"candidates,omitempty"`
	}
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return nil, fmt.Errorf("json unmarshal fail. %w", err)
	}
	return &TopicShiftDetectedResult{
		Topics:      params.Topics,
		Recommended: params.Recommended,
		Candidates:  params.Candidates,
	}, nil
}

// TopicShiftDetectedToolDefinition returns the ToolDefinition for the topic_shift_detected tool.
// Currently only the definition is retained; it is not yet in actual use.
func TopicShiftDetectedToolDefinition(lang string) llm.ToolDefinition {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"topics": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": i18n.TL(lang, "topic_shift_detected_param_topics_desc"),
			},
			"recommended": map[string]any{
				"type":        "string",
				"description": i18n.TL(lang, "topic_shift_detected_param_recommended_desc"),
			},
			"candidates": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": i18n.TL(lang, "topic_shift_detected_param_candidates_desc"),
			},
		},
		"additionalProperties": false,
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal topic_shift_detected schema: %v", err))
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(schemaBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to parse topic_shift_detected schema: %v", err))
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TopicShiftDetectedToolName,
			Description: i18n.TL(lang, "topic_shift_detected_tool_description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}

// TopicShiftDetectedToolImp implements llm.ToolIMP for the topic_shift_detected tool.
type TopicShiftDetectedToolImp struct {
	def    llm.ToolDefinition
	lang   string
	result *TopicShiftDetectedResult
}

// MakeTopicShiftDetectedTool creates a TopicShiftDetectedToolImp.
func MakeTopicShiftDetectedTool(lang string) *TopicShiftDetectedToolImp {
	return &TopicShiftDetectedToolImp{
		def:  TopicShiftDetectedToolDefinition(lang),
		lang: lang,
	}
}

var _ llm.ToolIMP = (*TopicShiftDetectedToolImp)(nil)

func (imp *TopicShiftDetectedToolImp) GetName() string {
	return TopicShiftDetectedToolName
}

func (imp *TopicShiftDetectedToolImp) GetDefinition() llm.ToolDefinition {
	return imp.def
}

func (imp *TopicShiftDetectedToolImp) SetArgument(arguments string) error {
	result, err := topicShiftDetectedArguments(arguments)
	if err != nil {
		return err
	}
	imp.result = result
	return nil
}

func (imp *TopicShiftDetectedToolImp) GetPendingText() string {
	return i18n.TL(imp.lang, "topic_shift_detected_tool_pending")
}

func (imp *TopicShiftDetectedToolImp) Execute() (string, error) {
	if imp.result == nil {
		return `{"status":"ok"}`, nil
	}

	result := map[string]any{
		"status":      "ok",
		"topics":      imp.result.Topics,
		"recommended": imp.result.Recommended,
	}
	resultBytes, _ := json.Marshal(result)
	return string(resultBytes), nil
}
