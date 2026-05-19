package agent

import (
	"fmt"

	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
)

// traitToolCaller implements llm.ToolCaller for trait extraction.
// It wraps llm.ToolIMP instances and provides the ToolCaller interface
// for executing tool calls made by the LLM during trait extraction.
type traitToolCaller struct {
	tools map[string]llm.ToolIMP
}

var _ llm.ToolCaller = (*traitToolCaller)(nil)

// newTraitToolCaller creates a traitToolCaller with the given tools.
func newTraitToolCaller(tools []llm.ToolIMP) *traitToolCaller {
	tc := &traitToolCaller{
		tools: make(map[string]llm.ToolIMP, len(tools)),
	}
	for _, t := range tools {
		tc.tools[t.GetName()] = t
	}
	return tc
}

// GetToolDefines returns the tool definitions from all managed tools.
func (tc *traitToolCaller) GetToolDefines() []llm.ToolDefinition {
	defs := make([]llm.ToolDefinition, 0, len(tc.tools))
	for _, imp := range tc.tools {
		defs = append(defs, imp.GetDefinition())
	}
	return defs
}

// Pending sets the arguments on the specified tool.
func (tc *traitToolCaller) Pending(toolCallID, toolName string, arguments string) error {
	imp, ok := tc.tools[toolName]
	if !ok {
		return fmt.Errorf("unknown tool '%s'", toolName)
	}
	return imp.SetArgument(arguments)
}

// Call executes the specified tool and returns the result.
func (tc *traitToolCaller) Call(toolCallID, toolName string) (string, error) {
	imp, ok := tc.tools[toolName]
	if !ok {
		return "", fmt.Errorf("unknown tool '%s'", toolName)
	}
	return imp.Execute()
}

// getTraits returns the extracted traits from the traits_extracted tool.
// Returns nil if the tool has not been called or no traits were extracted.
func (tc *traitToolCaller) getTraits() []toolimp.TraitsExtractedResult {
	imp, ok := tc.tools[toolimp.TraitsExtractedToolName]
	if !ok {
		return nil
	}
	traitsImp, ok := imp.(*toolimp.TraitsExtractedToolImp)
	if !ok {
		return nil
	}
	return traitsImp.GetTraits()
}
