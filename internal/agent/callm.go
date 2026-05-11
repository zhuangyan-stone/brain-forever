package agent

import (
	"context"
	"fmt"
	"log"

	"BrainOnline/infra/httpx/sse"
	"BrainOnline/infra/llm"
	"BrainOnline/internal/agent/toolimp"
)

// ============================================================
// Agent implementation — ChatHandler executes tools for DeepSeek
// ============================================================

// Tool implements llm.Agent.
// It dispatches tool calls by name to the appropriate handler.
// The returned messages are translated according to the handler's default language.
type agentImp struct {
	agent     *ChatAgent
	sseWriter sse.Writer

	ctx context.Context

	tools map[string]llm.ToolIMP
}

var _ llm.Agent = (*agentImp)(nil)

func NewAgentImp()

func (atc *agentImp) OnReasoning(subject, text string) {
	atc.sseWriter.WriteEvent(SSEEvent{
		Type:    "reasoning",
		Subject: subject,
		Content: text,
	})
}

func (atc *agentImp) OnWebSource(data any) {
	if data == nil {
		return
	}

	sources := data.([]toolimp.WebSource)
	if sources == nil {
		return
	}

	dst := make([]toolimp.WebSource, 0, len(sources))
	set := make(map[string]bool, len(sources))

	for _, page := range sources {
		if page.URL == "" {
			dst = append(dst, page)
			continue
		}

		if set[page.URL] {
			continue
		}

		set[page.URL] = true
		dst = append(dst, page)
	}

	if err := atc.sseWriter.WriteEvent(SSEEvent{
		Type:       "sources",
		WebSources: dst,
	}); err != nil {
		log.Printf("failed to write web sources event: %v", err)
	}
}

func (atc *agentImp) OnText(text string) {
	if err := atc.sseWriter.WriteEvent(SSEEvent{
		Type:    "text",
		Content: text,
	}); err != nil {
		log.Printf("failed to write web sources event: %v", err)
	}
}

func (ate *agentImp) OnError(err error) {
	e := ate.sseWriter.WriteEvent(SSEEvent{
		Type:    "error",
		Message: fmt.Sprintf("%v", err),
	})

	if e != nil {
		log.Printf("failed to write error event: %v", e)
	}
}

func (atc *agentImp) GetToolDefines() []llm.ToolDefinition {
	toolDefs := make([]llm.ToolDefinition, 0, len(atc.tools))

	for _, imp := range atc.tools {
		toolDefs = append(toolDefs, imp.GetDefinition())
	}

	return toolDefs
}

func (atc *agentImp) SetArgument(toolName, argument string) error {
	if imp, ok := atc.tools[toolName]; !ok {
		return fmt.Errorf("Unknown tool '%s'", toolName)
	} else {
		return imp.SetArgument(argument)
	}
}

func (atc *agentImp) Pending(toolName string) {
	if imp, ok := atc.tools[toolName]; !ok {
		return
	} else if pending := imp.GetPendingText(); pending == "" {
		return
	} else {
		atc.OnReasoning("pending", pending)
	}
}

func (atc *agentImp) Call(toolName, callID string) (string, error) {
	if imp, ok := atc.tools[toolName]; !ok {
		return "", fmt.Errorf("unknown tool '%s'", toolName)
	} else if result, err := imp.Execute(); err != nil {
		return "", err
	} else if imp.GetName() == toolimp.WebSearchToolName {
		if result == "" {
			return "", nil // nofound
		}

		searchImp := imp.(*toolimp.WebSearchToolImp)
		if searchImp == nil {
			return "", fmt.Errorf("bad tool call. type miss '%s'", toolName)
		}

		if len(searchImp.WebPages) == 0 {
			return "", nil // nofound
		}

		atc.OnWebSource(searchImp.WebPages)
		return result, nil
	} else {
		return result, nil
	}
}

// ============================================================
// LLM Streaming Call — delegates to DeepSeekRaw
// ============================================================

// performLLMStreamingCall performs a streaming LLM call with tool support.
// It delegates to DeepSeekRaw.PerformLLMStreamingCall, which handles the
// tool call loop internally.
func (h *ChatAgent) performLLMStreamingCall(
	ctx context.Context,
	sseWriter *sse.Writer,
	requestMsgID int64,
	messages_in []llm.Message,
	tools []llm.ToolIMP,
	deepThink bool,
) (fullReply string, messages_out []llm.Message, err error) {

	// Delegate to DeepSeekRaw
	reply, reasoningContent, err := h.charLLMClient.PerformLLMStreamingCall(ctx,
		messages, tools, h, deepThink)

	// Clear the collector
	h.webPagesCollector = nil

	if err != nil {
		return "", "", pages, err
	}

	return reply, reasoningContent, pages, nil
}
