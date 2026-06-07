package llm

type Role string

const (
	//系统指令，给ai定义的规矩
	RoleSystem Role = "system"
	//用户说的 / 工具结果
	RoleUser Role = "user"
	//ai的回复
	RoleAssistant Role = "assistant"
	//工具执行的结果
	RoleTool Role = "tool"
)

// Message的一条对话消息
type Message struct {
	//定义json字段名字
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// AI返回工具调用
type ToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
}

// 输入输出token
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// 流式相应的chunk_id
type ToolCallChunk struct {
	ID           string
	FunctionName string
	Arguments    string
}
