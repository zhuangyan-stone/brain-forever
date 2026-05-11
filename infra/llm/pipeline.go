package llm

type Pipeline interface {
	ToolCaller
	SSEResponser
}
