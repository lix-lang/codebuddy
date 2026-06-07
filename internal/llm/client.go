package llm

import "context"

// LLMClient LLM 客户端接口
// 所有提供商（OpenAI/Claude/DeepSeek/GLM/Ollama）都实现这个接口
type LLMClient interface {
	// ChatStream 流式调用 LLM
	// messages 是完整对话历史，tools 是可用的工具定义
	// 返回 channel，LLM 回复以 StreamEvent 逐个推出
	ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition) (<-chan StreamEvent, error)

	// CountTokens 估算消息消耗多少 token
	CountTokens(messages []Message) (int, error)

	// ModelName 返回当前模型名，如 "glm-4"
	ModelName() string

	// MaxContextTokens 返回模型最大上下文窗口（token 数）
	MaxContextTokens() int

	// SupportsVision 模型是否支持图片输入
	SupportsVision() bool
}

// ToolDefinition 工具定义，发给 LLM 的 function calling 格式
type ToolDefinition struct {
	Type     string       `json:"type"`     // 固定 "function"
	Function ToolFunction `json:"function"` // 函数描述
}

// ToolFunction 函数的描述和参数定义
type ToolFunction struct {
	Name        string         `json:"name"`        // 工具名，如 "read_file"
	Description string         `json:"description"` // 工具描述
	Parameters  map[string]any `json:"parameters"`  // JSON Schema 格式的参数定义
}
