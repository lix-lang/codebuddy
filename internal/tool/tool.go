package tool

import "context"

// ToolResult 工具执行的结果
type ToolResult struct {
	Output  string // 工具的输出内容（成功是结果，失败是错误信息）
	IsError bool   // 是否执行失败
}

// Tool 所有内置工具必须实现的接口
// Agent 循环中：LLM 返回 ToolCall → Agent 找到对应 Tool → Validate → Execute → 结果喂回 LLM
type Tool interface {
	// Name 工具名，如 "read_file"、"edit_file"
	// 这个名字会和 LLM Function Calling 的 function name 对应
	Name() string

	// Description 工具描述，告诉 LLM 这个工具能干什么、什么时候该用
	Description() string

	// Parameters 返回 JSON Schema 格式的参数定义
	// 告诉 LLM 这个工具接受什么参数、每个参数的类型和含义
	Parameters() map[string]any

	// Validate 执行前的参数校验（幻觉防护第二层）
	// 检查参数是否合法：路径是否存在、类型是否正确等
	Validate(args map[string]any) error

	// Execute 真正执行工具操作
	// ctx 用于超时控制和取消，args 是 LLM 传来的参数（已经过 Validate 校验）
	Execute(ctx context.Context, args map[string]any) (*ToolResult, error)

	// IsDestructive 标记这个工具是否会修改文件系统
	// true = 需要用户确认（write_file, edit_file, run_command）
	// false = 只读操作，不需要确认（read_file, search_code, analyze）
	IsDestructive() bool
}

// ToolRegistry 工具注册中心
// 所有工具启动时注册到这里，Agent 通过名字查找工具
type ToolRegistry interface {
	// Register 注册一个工具（启动时调用）
	Register(tool Tool)

	// Get 按名字查找工具（Agent 循环中 LLM 返回 ToolCall 时调用）
	Get(name string) (Tool, bool)

	// List 返回所有已注册的工具（构建 function definitions 时用）
	List() []Tool
}
