package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/session"
	"BrainForever/toolset"
)

// ============================================================
// pipelineImp -ChatHandler executes tools for DeepSeek
// ============================================================

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
	atc.sseWriter.WriteEvent(ReasoningEvent{
		Type:    "reasoning",
		Content: reasoning,
	})
}

func (atc *pipelineImp) OnReasoningEnd() {
	atc.sseWriter.WriteEvent(ReasoningEndEvent{
		Type: "reasoning_end",
	})
}

func (atc *pipelineImp) OnToolReasoning(subject, toolName, text string) {
	atc.sseWriter.WriteEvent(ReasoningEvent{
		Type:    "reasoning",
		Subject: subject,
		Tool:    toolName,
		Content: text,
	})
}

func (atc *pipelineImp) OnText(text string) {
	if err := atc.sseWriter.WriteEvent(TextEvent{
		Type:    "text",
		Content: text,
	}); err != nil {
		atc.agent.logger.Errorf("failed to write text event. %v", err)
	}
}

func (ate *pipelineImp) OnError(err error) {
	e := ate.sseWriter.WriteEvent(ErrorEvent{
		Type:    "error",
		Message: i18n.TL(ate.lang, "server_error", map[string]any{"Error": err.Error()}),
	})

	if e != nil {
		ate.agent.logger.Errorf("failed to write error event. %v", e)
	}
}

func (atc *pipelineImp) OnWebSource(sources []toolimp.WebSource) {
	// WebSource is a type alias for llmtypes.WebSource, so this works directly
	if err := atc.sseWriter.WriteEvent(WebSourceEvent{
		Type:       "web_source",
		WebSources: sources,
	}); err != nil {
		atc.agent.logger.Errorf("failed to write web sources event. %v", err)
	}
}

func (ate *pipelineImp) GetWebSearchResult() (sources []toolimp.WebSource) {
	const maxSources = 100
	urlSet := make(map[string]bool, maxSources)
	sources = make([]toolimp.WebSource, 0, maxSources)

	for _, tl := range ate.tools {
		if tl.GetName() == toolimp.WebSearchToolName {
			if searcherTl := tl.(*toolimp.WebSearchToolImp); searcherTl != nil {
				for _, page := range searcherTl.WebPages {
					if len(sources) >= maxSources {
						break
					}

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
// LLM Streaming Call
// ============================================================

// callLLMWithPipeline performs a streaming LLM call with tool support.
// sess is used to get the user's provider and personal API key.
func (h *ChatAgent) callLLMWithPipeline(
	ctx context.Context,
	sseWriter *sse.Writer,
	userMsgID int64,
	messages []llm.Message,
	tools []llm.ToolIMP,
	withDeepThink bool,
	lang string,
	sess *session.Session,
) *Message {
	pipeline := MakePipeline(ctx, h, sseWriter, tools, lang)

	client := sessionLLMClient(sess)
	apiSetting := sessionLLMApiSetting(sess)

	reply, reasoning, err := client.ChatWithPipeline(ctx,
		messages, &pipeline, withDeepThink, apiSetting.ApiKey)

	isInterrupted := ctx.Err() != nil

	var usage *Usage
	var assistantMsg *Message

	if err != nil && !isInterrupted {
		pipeline.OnError(err)

		assistantMsg = &Message{
			ID:          userMsgID,
			Role:        llm.RoleAssistant,
			Content:     reply,
			Reasoning:   reasoning,
			CreatedAt:   time.Now().UTC(),
			Interrupted: 2,
		}
	} else {
		isEstimated := true
		var promptTokens, completionTokens int = -1, -1

		if usage := client.GetUsageInfo(); usage != nil {
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

		if len(reply) > 0 || isInterrupted {
			assistantMsg = &Message{
				ID:        userMsgID,
				Role:      llm.RoleAssistant,
				Content:   reply,
				Reasoning: reasoning,
				CreatedAt: time.Now().UTC(),
				Usage:     usage,
				Interrupted: func() int {
					if isInterrupted {
						return 1
					}
					return 0
				}(),
			}

			webPages := pipeline.GetWebSearchResult()

			if len(webPages) > 0 {
				assistantMsg.Sources = webPages
			}

			pipeline.OnWebSource(webPages)
		}
	}

	createdAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	sseWriter.WriteEvent(DoneEvent{
		Type:      "done",
		Usage:     usage,
		MsgID:     userMsgID,
		CreatedAt: createdAt,
	})

	return assistantMsg
}

func mergeMessagesContent(messages []llm.Message) string {
	var content strings.Builder
	for _, msg := range messages {
		content.WriteString(msg.Content)
	}

	return content.String()
}
