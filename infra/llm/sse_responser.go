package llm

type SSEResponser interface {
	OnReasoning(subject, text string)
	OnWebSource(data any)
	OnText(text string)
	OnError(err error)
}
