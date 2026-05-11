package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"BrainOnline/infra/httpx/sse"
	"BrainOnline/infra/i18n"
	"BrainOnline/infra/llm"
	"BrainOnline/internal/agent/toolimp"
	"BrainOnline/toolset"
)

// ============================================================
// Agent implementation — ChatHandler executes tools for DeepSeek
// ============================================================

// Tool implements llm.Agent.
// It dispatches tool calls by name to the appropriate handler.
// The returned messages are translated according to the handler's default language.
type pipelineImp struct {
	ctx context.Context

	agent     *ChatAgent
	sseWriter *sse.Writer

	tools map[string]llm.ToolIMP
	lang  string
}

var _ llm.Pipeline = (*pipelineImp)(nil)

func MakePipeline(ctx context.Context, agent *ChatAgent, sseWriter *sse.Writer, tools []llm.ToolIMP, lang string) pipelineImp {
	pipeline := pipelineImp{
		ctx:       ctx,
		agent:     agent,
		sseWriter: sseWriter,
		tools:     make(map[string]llm.ToolIMP, len(tools)),
		lang:      lang,
	}

	for _, t := range tools {
		pipeline.tools[t.GetName()] = t
	}

	return pipeline
}

func (atc *pipelineImp) OnReasoning(reasoning string) {
	atc.sseWriter.WriteEvent(SSEEvent{
		Type:    "reasoning",
		Content: reasoning,
	})
}

func (atc *pipelineImp) OnReasoningEnd() {
	atc.sseWriter.WriteEvent(SSEEvent{
		Type: "reasoning_end",
	})
}

func (atc *pipelineImp) OnToolReasoning(subject, toolName, text string) {
	atc.sseWriter.WriteEvent(SSEEvent{
		Type:    "reasoning",
		Subject: subject,
		Tool:    toolName,
		Content: text,
	})
}

func (atc *pipelineImp) OnText(text string) {
	if err := atc.sseWriter.WriteEvent(SSEEvent{
		Type:    "text",
		Content: text,
	}); err != nil {
		log.Printf("failed to write web sources event: %v", err)
	}
}

func (ate *pipelineImp) OnError(err error) {
	e := ate.sseWriter.WriteEvent(SSEEvent{
		Type:    "error",
		Message: i18n.TL(ate.lang, "server_error", map[string]interface{}{"Error": err.Error()}),
	})

	if e != nil {
		log.Printf("failed to write error event: %v", e)
	}
}

func (atc *pipelineImp) OnWebSource(sources []toolimp.WebSource) {
	if err := atc.sseWriter.WriteEvent(SSEEvent{
		Type:       "sources",
		WebSources: sources,
	}); err != nil {
		log.Printf("failed to write web sources event: %v", err)
	}
}

func (ate *pipelineImp) GetWebSearchResult() (sources []toolimp.WebSource) {
	urlSet := make(map[string]bool, 50)
	sources = make([]toolimp.WebSource, 0, 50)

	for _, tl := range ate.tools {
		if tl.GetName() == toolimp.WebSearchToolName {
			if searcherTl := tl.(*toolimp.WebSearchToolImp); searcherTl != nil {
				// 去重
				for _, page := range searcherTl.WebPages {
					url := page.URL
					if url == "" {
						sources = append(sources, page)
						continue
					}

					if urlSet[page.URL] {
						continue
					}

					urlSet[url] = true
					sources = append(sources, page)
				}
			}
		}
	}

	return
}

func (atc *pipelineImp) GetToolDefines() []llm.ToolDefinition {
	toolDefs := make([]llm.ToolDefinition, 0, len(atc.tools))

	for _, imp := range atc.tools {
		toolDefs = append(toolDefs, imp.GetDefinition())
	}

	return toolDefs
}

func (atc *pipelineImp) Pending(toolCallID, toolName string, argument string) error {
	if imp, ok := atc.tools[toolName]; !ok {
		return fmt.Errorf("unknown tool '%s'", toolName)
	} else if err := imp.SetArgument(argument); err != nil {
		return fmt.Errorf("set argument fail. tool: '%s'. argument: '%s'. %w", toolName, argument, err)
	} else if pending := imp.GetPendingText(); pending == "" {
		return nil
	} else {
		atc.OnToolReasoning("tool-pending", toolName, pending)
	}

	return nil
}

func (atc *pipelineImp) Call(toolCallID, toolName string) (string, error) {
	if imp, ok := atc.tools[toolName]; !ok {
		return "", fmt.Errorf("unknown tool '%s'", toolName)
	} else if result, err := imp.Execute(); err != nil {
		return "", err
	} else {
		return result, nil
	}
}

// ============================================================
// LLM Streaming Call — delegates to DeepSeekRaw
// ============================================================

// callLLMWithPipeline performs a streaming LLM call with tool support.
// It delegates to DeepSeek's imp, which handles the tool call loop internally.
func (h *ChatAgent) callLLMWithPipeline(
	ctx context.Context,
	session *session,
	sseWriter *sse.Writer,
	userMsgID int64,
	messages []llm.Message,
	tools []llm.ToolIMP,
	withDeepThink bool,
	lang string,
) {
	// construct pipeline
	pipeline := MakePipeline(ctx, h, sseWriter, tools, lang)

	// Delegate to DeepSeekRaw
	reply, reasoning, err := h.charLLMClient.ChatWithPipeline(ctx,
		messages, &pipeline, withDeepThink)

	var usage *Usage

	if err != nil {
		pipeline.OnError(err) // Display "Oops! Server error!\n %v"
	} else {
		// Get or manually (simulate) calculate the tokens consumed in this interaction
		isEstimated := true
		var promptTokens, completionTokens int = -1, -1

		if usage := h.charLLMClient.GetUsageInfo(); usage != nil {
			if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
				isEstimated = false
			}

			if usage.PromptTokens > 0 {
				promptTokens = usage.PromptTokens
			}
			if usage.CompletionTokens > 0 {
				completionTokens = usage.CompletionTokens
			}
		}

		// If the API didn't provide token counts, use simple estimation
		if promptTokens == -1 {
			promptTokens = toolset.TokenEstimate(mergeMessagesContent(messages))
		}
		if completionTokens == -1 {
			completionTokens = toolset.TokenEstimate(reply)
		}

		usage = &Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
			IsEstimated:      isEstimated,
		}

		// Append the LLM's full reply to the user's internal history
		//     The AI reply reuses the user message's ID (source ID)
		if len(reply) > 0 {
			assistantMsg := Message{
				ID:        userMsgID, // same as user message's id
				Role:      "assistant",
				Content:   reply,
				Reasoning: reasoning,
				CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
				Usage:     usage,
			}

			// Attach web search sources so they can be restored after page refresh
			webPages := pipeline.GetWebSearchResult()

			if len(webPages) > 0 {
				assistantMsg.Sources = webPages
			}

			appendNewResponseMessage(session, &assistantMsg)

			pipeline.OnWebSource(webPages)
		}
	}

	sseWriter.WriteEvent(SSEEvent{
		Type:  "done",
		Usage: usage,
		MsgID: userMsgID,
	})
}

func mergeMessagesContent(messages []llm.Message) string {
	var content strings.Builder
	for _, msg := range messages {
		content.WriteString(msg.Content)
	}

	return content.String()
}
