package agent

import (
	"encoding/json"
	"fmt"
	"sync"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/llm"
	"BrainForever/internal/remote/agent/toolimp"
)

// ============================================================
// sseEvent — JSON structure for each SSE data message
// ============================================================

type sseEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// ============================================================
// TraitSSEResponser — implements llm.SSEResponser
//
// Wraps an SSE writer and forwards LLM streaming events (reasoning,
// text, errors, tool calls) to the frontend as structured JSON SSE events.
// ============================================================

// TraitSSEResponser implements llm.SSEResponser using an SSE writer.
type TraitSSEResponser struct {
	sseWriter *sse.Writer
	mu        sync.Mutex
}

// Compile-time interface check.
var _ llm.SSEResponser = (*TraitSSEResponser)(nil)

// NewTraitSSEResponser creates a new TraitSSEResponser.
func NewTraitSSEResponser(sw *sse.Writer) *TraitSSEResponser {
	return &TraitSSEResponser{sseWriter: sw}
}

// WriteEvent marshals and writes a structured SSE event.
// Exported so callers can send custom event types (e.g., tool_call, done).
func (r *TraitSSEResponser) WriteEvent(eventType string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	msg := sseEvent{Event: eventType, Data: data}
	b, _ := json.Marshal(msg)
	_ = r.sseWriter.WriteRaw(string(b))
}

// OnReasoning forwards reasoning content to the frontend.
func (r *TraitSSEResponser) OnReasoning(reasoning string) {
	r.WriteEvent("reasoning", reasoning)
}

// OnToolReasoning forwards tool reasoning to the frontend.
func (r *TraitSSEResponser) OnToolReasoning(subject, toolName, text string) {
	r.WriteEvent("tool_reasoning", map[string]string{
		"subject":  subject,
		"toolName": toolName,
		"text":     text,
	})
}

// OnReasoningEnd signals the end of reasoning phase.
func (r *TraitSSEResponser) OnReasoningEnd() {
	r.WriteEvent("reasoning_end", nil)
}

// OnText forwards text content to the frontend.
func (r *TraitSSEResponser) OnText(text string) {
	r.WriteEvent("text", text)
}

// OnError forwards an error message to the frontend.
func (r *TraitSSEResponser) OnError(err error) {
	r.WriteEvent("error", err.Error())
}

// ============================================================
// TraitPipeline — implements llm.Pipeline
//
// Combines TraitSSEResponser (SSE streaming) with ToolCaller
// (tool execution) into a single Pipeline implementation.
// ============================================================

// TraitPipeline implements llm.Pipeline by embedding SSEResponser
// and providing ToolCaller methods via a tool registry.
type TraitPipeline struct {
	*TraitSSEResponser
	tools  map[string]llm.ToolIMP
	result *toolimp.TripTraitsTool // stores the final extraction result
}

// Compile-time interface check.
var _ llm.Pipeline = (*TraitPipeline)(nil)

// NewTraitPipeline creates a TraitPipeline with the given SSE writer and tools.
func NewTraitPipeline(sw *sse.Writer, tools []llm.ToolIMP) *TraitPipeline {
	p := &TraitPipeline{
		TraitSSEResponser: NewTraitSSEResponser(sw),
		tools:             make(map[string]llm.ToolIMP, len(tools)),
	}
	for _, t := range tools {
		p.tools[t.GetName()] = t
		if imp, ok := t.(*toolimp.TripTraitsTool); ok {
			p.result = imp
		}
	}
	return p
}

// GetToolDefines returns all tool definitions from registered tools.
func (p *TraitPipeline) GetToolDefines() []llm.ToolDefinition {
	defs := make([]llm.ToolDefinition, 0, len(p.tools))
	for _, imp := range p.tools {
		defs = append(defs, imp.GetDefinition())
	}
	return defs
}

// Pending sets the arguments on the specified tool.
func (p *TraitPipeline) Pending(toolCallID, toolName string, arguments string) error {
	imp, ok := p.tools[toolName]
	if !ok {
		return fmt.Errorf("unknown tool '%s'", toolName)
	}
	return imp.SetArgument(arguments)
}

// Call executes the specified tool and returns the result.
func (p *TraitPipeline) Call(toolCallID, toolName string) (string, error) {
	imp, ok := p.tools[toolName]
	if !ok {
		return "", fmt.Errorf("unknown tool '%s'", toolName)
	}
	return imp.Execute()
}

// GetResult returns the TripTraitsTool holding the final extraction result.
func (p *TraitPipeline) GetResult() *toolimp.TripTraitsTool {
	return p.result
}
