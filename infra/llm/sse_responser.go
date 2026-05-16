package llm

type SSEResponser interface {
	OnReasoning(reasoning string)
	OnToolReasoning(subject, toolName, text string)
	OnReasoningEnd()

	OnText(text string)
	OnError(err error)
}
