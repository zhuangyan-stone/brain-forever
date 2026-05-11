package llm

type SSEResponser interface {
	OnReasoning(reasoning string)
	OnToolReasoning(subject, toolName, text string)

	OnText(text string)
	OnError(err error)
}
