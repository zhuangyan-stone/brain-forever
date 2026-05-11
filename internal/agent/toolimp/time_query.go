package toolimp

import (
	"BrainOnline/infra/i18n"
	"BrainOnline/infra/llm"
	"encoding/json"
	"fmt"
	"time"
)

// TimeQueryToolName is the name of the tool used for time query.
// The LLM can call this tool when it determines that currernt local time is needed.
const TimeQueryToolName = "get_current_local_time"

// getCurrentLocalTimeWithZone : return like 2026-05-10 14:30:00+08:00 [Asia/Shanghai]
func getCurrentLocalTimeWithZone() string {
	now := time.Now()
	timePart := now.Format("2006-01-02 15:04:05-07:00")
	tzName := now.Location().String()
	return fmt.Sprintf("%s [%s]", timePart, tzName)
}

// timeQueryToolDefinition returns the ToolDefinition for time query
// using llm types, with translated descriptions.
//
// This tool takes no parameters — the LLM simply calls it to get the
// current local time with timezone information.
func timeQueryToolDefinition(lang string) llm.ToolDefinition {
	// Build the schema as a Go map and marshal it to JSON.
	// Using json.Marshal ensures the description string is properly escaped
	// (e.g., double quotes, newlines, etc.), so any translation content is safe.
	//
	// This tool has no parameters (empty properties), so the LLM just calls
	// it directly without any arguments.
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []string{},
		"additionalProperties": false,
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal time query schema: %v", err))
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(schemaBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to parse time query schema: %v", err))
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TimeQueryToolName,
			Description: i18n.TL(lang, "time_query_tool_description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}

// ExecuteTimeQuery performs the actual get current local time and returns the results.
func executeTimeQuery() (currentLocalTimeWithZone string) {
	return getCurrentLocalTimeWithZone()
}

type TimeQueryToolImp struct {
	def  llm.ToolDefinition
	lang string
}

func MakeTimeQueryTool(lang string) *TimeQueryToolImp {
	return &TimeQueryToolImp{def: timeQueryToolDefinition(lang), lang: lang}
}

var _ llm.ToolIMP = (*TimeQueryToolImp)(nil)

func (f *TimeQueryToolImp) GetName() string {
	return TimeQueryToolName
}

func (f *TimeQueryToolImp) GetDefinition() llm.ToolDefinition {
	return f.def
}

func (f *TimeQueryToolImp) SetArgument(arguments string) error {
	return nil
}

func (f *TimeQueryToolImp) GetPendingText() string {
	return i18n.TL(f.lang, "time_query_tool_pending")
}

func (f *TimeQueryToolImp) Execute() (string, error) {
	return getCurrentLocalTimeWithZone(), nil
}
