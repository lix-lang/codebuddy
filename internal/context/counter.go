package context

import "github.com/lix-lang/codebuddy/internal/llm"

// EstimateMessagesToken 估算一组消息的 token 数
// 用于上下文管理：判断对话历史是否快超预算了
//
// 估算规则（粗略但够用）：
//   - 英文约 4 个字符 = 1 token
//   - 中文约 1 个字 ≈ 2 token
//   - 取平均：每 3 个字符约 1 token
//   - 每条消息额外 4 token 开销（角色标签、分隔符等）
//
// 注意：这只是估算，不是精确值。精确值需要用 tiktoken 等库，
// 但对于上下文管理来说，估算够用了，误差在可接受范围内。
func EstimateMessagesToken(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		// 消息内容的 token 估算
		total += len(msg.Content) / 3

		// 工具调用参数的 token 估算
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Name) / 3
			total += len(tc.Function.Arguments) / 3
		}

		// 每条消息的格式开销（role: "user"\n, content: "..."\n 等）
		total += 4
	}
	return total
}

// EstimateTextToken 估算纯文本的 token 数
// 用于估算文件内容、system prompt 等的 token 消耗
func EstimateTextToken(text string) int {
	return len(text) / 3
}

// TokenBudget token 预算分配
// 上下文窗口有限，需要合理分配给各个部分
type TokenBudget struct {
	Total        int // 总预算（模型的上下文窗口大小）
	SystemPrompt int // System Prompt 分配的 token（约 2K）
	ProjectInfo  int // 项目概览分配的 token（约 1K）
	Tools        int // 工具定义分配的 token（约 3K）
	Output       int // 预留给 LLM 输出的 token（约 22K，占总预算 17%）
	Files        int // 文件内容可用的 token（弹性）
	History      int // 对话历史可用的 token（弹性）
}

// CalculateBudget 根据模型上下文窗口计算 token 预算分配
// maxContext 是模型的最大上下文窗口（如 128000）
func CalculateBudget(maxContext int) TokenBudget {
	// 固定部分
	systemPrompt := 2000
	projectInfo := 1000
	tools := 3000
	output := int(float64(maxContext) * 0.17) // 预留 17% 给输出

	// 弹性部分 = 总预算 - 固定部分
	elastic := maxContext - systemPrompt - projectInfo - tools - output
	if elastic < 0 {
		elastic = 0
	}

	// 弹性部分按 6:4 分给文件和历史
	files := elastic * 6 / 10
	history := elastic * 4 / 10

	return TokenBudget{
		Total:        maxContext,
		SystemPrompt: systemPrompt,
		ProjectInfo:  projectInfo,
		Tools:        tools,
		Output:       output,
		Files:        files,
		History:      history,
	}
}

// Remaining 返回剩余可用的弹性 token
// used 是已使用的 token 数
func (b TokenBudget) Remaining(used int) int {
	r := b.Files + b.History - used
	if r < 0 {
		return 0
	}
	return r
}
