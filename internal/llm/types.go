package llm

// Role 消息角色类型，本质是 string，但独立类型防止传错
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

// Message 一条对话消息，Agent 和 LLM 之间的所有通信都通过 Message 传递
type Message struct {
	Role       Role       `json:"role"`                   // 谁发的（system/user/assistant/tool）
	Content    string     `json:"content"`                // 说的内容（文字部分）
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // AI 发起的工具调用（仅 assistant 有）
	ToolCallID string     `json:"tool_call_id,omitempty"` // 工具结果回执 ID（仅 tool 有）
	Name       string     `json:"name,omitempty"`         // 工具名（仅 tool 有）
}

// ToolCall AI 发起的一次工具调用
type ToolCall struct {
	ID       string   `json:"id"` // 调用 ID，用来把工具结果对应回去
	Function struct { // 匿名嵌套结构体
		Name      string `json:"name"`      // 工具名，如 "read_file"
		Arguments string `json:"arguments"` // 工具参数，JSON 字符串，如 '{"path":"main.go"}'
	}
}

// Usage 每次 API 调用的 token 消耗统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`     // 输入消耗的 token
	CompletionTokens int `json:"completion_tokens"` // 输出消耗的 token
	TotalTokens      int `json:"total_tokens"`      // 总计
}

// ToolCallChunk SSE 流式响应中的工具调用碎片
// 一个工具调用的参数可能分成多个 chunk，按 ID 拼接成完整参数
type ToolCallChunk struct {
	ID             string // 工具调用 ID，用来把碎片归到同一个调用
	FunctionName   string // 工具名（只有第一块有值，后面的是空字符串）
	ArgumentsDelta string // 这一块的参数碎片，拼接起来才是完整 JSON
}

// StreamEventType 流式事件类型
type StreamEventType int

const (
	// 0 - LLM 输出了一段文字
	StreamEventContent StreamEventType = iota
	// 1 - 工具调用碎片（参数还没拼完）
	StreamEventToolCall
	// 2 - 流结束，附带 token 用量
	StreamEventDone
)

// StreamEvent LLM 客户端通过 channel 推出的一个事件
type StreamEvent struct {
	// 哪种事件
	Type StreamEventType
	// 文本片段（Type == StreamEventContent 时有值）
	Content string
	// 工具调用碎片（Type == StreamEventToolCall 时有值）
	ToolCalls *ToolCallChunk
	// token 用量（Type == StreamEventDone 时有值）
	Usage *Usage
}
