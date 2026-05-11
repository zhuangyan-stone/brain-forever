package llm

// ToolIMP provided the actual implementer of a tool definition.
type ToolIMP interface {
	// Get tool function's Name
	GetName() string

	// GetDefinition returns the tool definition to be provided to the LLM.
	GetDefinition() ToolDefinition

	SetArgument(arguments string) error

	// GetPendingText generates the reason for calling the tool (typically used to show the LLM's thought process to the user).
	GetPendingText() string

	// Execute runs the tool and returns the result (usually in natural language).
	Execute() (string, error)
}

// ============================================================
// ToolCaller interface — decouples tool execution from the LLM client
// ============================================================

// ToolCaller is the interface for executing tool calls made by the LLM.
// The caller (e.g., ChatHandler) implements this interface to provide
// concrete tool implementations (e.g., web_search).
type ToolCaller interface {
	GetToolDefines() []ToolDefinition

	// 设置函数调用入参（通常由LLM生成），必要的话，借助sse发送给前端
	Pending(toolCallID, toolName string, arguments string) error

	// Execute tool function and returns the result content.
	// toolName is the function name (e.g., "web_search").
	// arguments is the JSON string of the tool call arguments.
	// The returned string is the tool result content sent back to the LLM.
	Call(toolCallID, toolName string) (result string, err error)
}
