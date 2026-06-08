package context

import (
	"fmt"

	"github.com/lix-lang/codebuddy/internal/llm"
)

// CountMessagesToken 精确计算一组消息的 token 数
// 使用模型原生的分词器计算，每个模型用自己的分词器：
//   - OpenAI → tiktoken（原生分词器）
//   - Qwen → qwen-tokenizer（专用库）
//   - GLM/DeepSeek → HuggingFace tokenizer.json
//
// model 是模型名，如 "glm-4"、"gpt-4o"，用来选择对应的分词器
//
// 每条消息的开销包括：
//   - 消息内容的 token 数（用对应模型的分词器精确计算）
//   - 消息格式的固定开销（角色标签、分隔符等，约 4 token/条）
//   - 工具调用的 token 数（函数名 + 参数）
func CountMessagesToken(model string, messages []llm.Message) int {
	// GetTokenCounter 根据模型名选择对应的分词器（带缓存）
	counter := GetTokenCounter(model)
	total := 0

	for _, msg := range messages {
		// 用模型原生的分词器精确计算消息内容的 token 数
		total += counter.CountTokens(msg.Content)

		// 工具调用的 token 数
		for _, toolCall := range msg.ToolCalls {
			total += counter.CountTokens(toolCall.Function.Name)
			total += counter.CountTokens(toolCall.Function.Arguments)
		}

		// 每条消息的格式开销（OpenAI API 格式的额外 token）
		// 包括 <|start|>{role}\n{content}<|end|> 这些标记
		total += 4
	}

	return total
}

// CountTextToken 精确计算纯文本的 token 数
// 用于计算文件内容、system prompt 等的 token 消耗
//
// model 是模型名，用来选择对应的分词器
func CountTextToken(model string, text string) int {
	tc := GetTokenCounter(model)
	return tc.CountTokens(text)
}

// EstimateMessagesToken 估算一组消息的 token 数（快速但不够精确）
// 在不需要精确值时使用（比如快速检查是否快超预算了）
// 作为 CountMessagesToken 的轻量替代
func EstimateMessagesToken(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		// 粗略估算：字符数 / 3 ≈ token 数
		total += len(msg.Content) / 3
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Name) / 3
			total += len(tc.Function.Arguments) / 3
		}
		total += 4
	}
	return total
}

// EstimateTextToken 估算纯文本的 token 数（快速但不够精确）
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

// TokenUsageInfo 返回人类可读的 token 使用情况
func (b TokenBudget) TokenUsageInfo(used int) string {
	return fmt.Sprintf("Token 使用: %d / %d (文件: %d, 历史: %d, 剩余: %d)",
		used, b.Files+b.History, b.Files, b.History, b.Remaining(used))
}
