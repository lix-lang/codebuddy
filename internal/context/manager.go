package context

import "github.com/lix-lang/codebuddy/internal/llm"

// ContextManager 上下文管理器
// 负责控制发给 LLM 的内容大小，确保不超过模型的 token 上限
type ContextManager interface {
	// BuildMessages 构建发给 LLM 的完整消息列表
	// 返回 system prompt + 项目概览 + 相关文件内容 + 对话历史 + 用户新消息
	BuildMessages(userMsg string) ([]llm.Message, error)

	// AddFile 把一个文件加入上下文
	AddFile(path string) error

	// RemoveFile 从上下文中移除一个文件（腾出 token 空间）
	RemoveFile(path string)

	// AddHistory 把一条消息加入对话历史
	AddHistory(msg llm.Message)

	// TokenUsage 返回当前上下文的 token 使用情况 (已用, 预算上限)
	TokenUsage() (used int, budget int)

	// Compact 压缩上下文（把早期对话压缩成摘要）
	Compact() error

	// Reset 清空上下文（新对话开始时调用）
	Reset()
}
