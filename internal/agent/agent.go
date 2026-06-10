package agent

import (
	"context"

	"github.com/lix-lang/codebuddy/internal/llm"
	"github.com/lix-lang/codebuddy/internal/tool"
)

// AgentState Agent 当前所处的状态
type AgentState int

const (
	// StateIdle 空闲，等待用户输入
	StateIdle AgentState = iota
	// StateThinking 正在调用 LLM
	StateThinking
	// StateExecuting 正在执行工具
	StateExecuting
	// StateConfirming 等待用户确认（破坏性操作）
	StateConfirming
	// StateDone 任务完成
	StateDone
	// StateError 出错了，无法继续
	StateError
)

// String 返回状态的字符串表示（用于日志和 TUI 显示）
func (s AgentState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateThinking:
		return "thinking"
	case StateExecuting:
		return "executing"
	case StateConfirming:
		return "confirming"
	case StateDone:
		return "done"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// Agent 编程助手的核心 Agent 接口
// ReAct 模式：Reasoning（LLM 思考）→ Acting（执行工具）→ 循环
type Agent interface {
	// Run 启动一次 Agent 循环，反复调用 LLM → 工具 → LLM，直到完成或达到 MaxSteps
	Run(ctx context.Context, userMsg string) error

	// State 返回当前 Agent 状态（供 TUI 显示）
	State() AgentState

	// Stop 中断正在运行的 Agent 循环（用户 Ctrl+C 时调用）
	Stop()

	// History 返回完整对话历史
	History() []llm.Message

	// RegisterTool 注册一个工具到 Agent
	RegisterTool(t tool.Tool)

	// SetOnToolCall 设置工具调用回调（TUI 用来实时显示工具执行进度）
	SetOnToolCall(func(name string, args map[string]any))

	// SetOnStateChange 设置状态变更回调（TUI 用来更新界面状态）
	SetOnStateChange(func(old, new AgentState))

	// SetOnError 设置错误回调（TUI 用来显示错误提示）
	SetOnError(func(err error))
}
