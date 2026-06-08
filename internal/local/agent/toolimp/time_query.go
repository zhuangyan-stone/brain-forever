package toolimp

import (
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"encoding/json"
	"fmt"
	"time"
)

// TimeQueryToolName is the name of the tool used for time query.
// The LLM can call this tool when it determines that currernt local time is needed.
const TimeQueryToolName = "current_time"

// getCurrentTime returns both local time (with timezone) and UTC time.
// Format:
//
//	Local : 2026-05-23 16:10:42 CST (Asia/Shanghai)
//	UTC   : 2026-05-23 08:10:42 UTC
func getCurrentTime() string {
	now := time.Now()
	localTime := now.Format("2006-01-02 15:04:05")
	tzName := now.Location().String()
	utcTime := now.UTC().Format("2006-01-02 15:04:05")
	return fmt.Sprintf("Local : %s %s\nUTC   : %s UTC", localTime, tzName, utcTime)
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
			Description: i18n.Tools.TL(lang, "current_time", "description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}

// ExecuteTimeQuery performs the actual get current time and returns the results.
func executeTimeQuery() (currentTime string) {
	return getCurrentTime()
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
	return i18n.Tools.TL(f.lang, "current_time", "pending")
}

func (f *TimeQueryToolImp) Execute() (string, error) {
	return getCurrentTime(), nil
}
