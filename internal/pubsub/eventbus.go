package pubsub

// EventType 事件类型
type EventType int

const (
	// EventContent LLM 流式文本内容
	EventContent EventType = iota
	// EventToolCall 工具调用开始
	EventToolCall
	// EventToolResult 工具调用结果
	EventToolResult
	// EventStateChange Agent 状态变更
	EventStateChange
	// EventError 错误
	EventError
	// EventUsageUpdate Token 使用量更新
	EventUsageUpdate
)

// Event 事件总线中的事件
type Event struct {
	// Type 事件类型
	Type EventType

	// Content 文本内容（EventContent 时使用）
	Content string

	// ToolName 工具名称（EventToolCall / EventToolResult 时使用）
	ToolName string

	// ToolArgs 工具参数摘要（EventToolCall 时使用）
	ToolArgs string

	// ToolResult 工具结果摘要（EventToolResult 时使用）
	ToolResult string

	// StateName 状态名称字符串（EventStateChange 时使用）
	// 使用 string 而非 agent.AgentState 避免循环导入
	// 可选值: "idle", "thinking", "executing", "confirming", "done", "error"
	StateName string

	// Err 错误（EventError 时使用）
	Err error

	// TotalTokens 本次会话累计 token（EventUsageUpdate 时使用）
	TotalTokens int

	// TotalCost 本次会话累计费用（EventUsageUpdate 时使用）
	TotalCost float64
}

// ConfirmRequest 确认请求，用于破坏性操作前的用户确认
type ConfirmRequest struct {
	// ToolName 工具名称
	ToolName string

	// Args 参数摘要
	Args string

	// Response 用户回复 chan：true 确认，false 拒绝
	Response chan bool
}

// EventBus 事件总线，解耦 Agent 和 TUI
//
// Agent 通过 Push 推送事件，TUI 通过 Subscribe 订阅消费。
// 使用带缓冲的 channel 防止 Agent 阻塞。
type EventBus struct {
	ch chan Event
}

// NewEventBus 创建事件总线
func NewEventBus() *EventBus {
	return &EventBus{
		ch: make(chan Event, 256),
	}
}

// Push 非阻塞推送事件
// 如果 channel 满了则丢弃（避免 Agent 阻塞）
func (eb *EventBus) Push(e Event) {
	select {
	case eb.ch <- e:
	default:
		// channel 满，丢弃事件
	}
}

// Subscribe 返回事件 channel（只读）
func (eb *EventBus) Subscribe() <-chan Event {
	return eb.ch
}
