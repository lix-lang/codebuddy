package context

import (
	"fmt"
	"sync"

	"github.com/lix-lang/codebuddy/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// tokenEncoder 全局的 tiktoken 编码器（单例，只初始化一次）
// tiktoken 是 OpenAI 开源的 BPE 分词器，能精确计算 token 数
// 不同模型用不同的编码方式：
//   - GPT-4 / GPT-4o / GPT-3.5-turbo → "cl100k_base"
//   - GLM / DeepSeek（兼容 OpenAI 格式）→ 也用 "cl100k_base"，误差很小
//   - Claude → 有自己的分词器，但 cl100k_base 的结果也很接近
var (
	encoder     *tiktoken.Tiktoken
	encoderOnce sync.Once // sync.Once 确保只初始化一次
)

// getEncoder 获取 tiktoken 编码器（懒加载，第一次调用时才初始化）
func getEncoder() *tiktoken.Tiktoken {
	// sync.Once.Do 保证里面的函数只执行一次，即使多个 goroutine 同时调用
	// 后续调用直接返回已初始化的 encoder，不再重复执行
	encoderOnce.Do(func() {
		var err error
		// tiktoken.EncodingForModel 根据模型名自动选择对应的编码方式
		// 如果模型名不认识，回退到 "cl100k_base"（GPT-4 使用的编码）
		encoder, err = tiktoken.EncodingForModel("gpt-4")
		if err != nil {
			// 回退到 cl100k_base 编码（适用于大多数 OpenAI 兼容模型）
			encoder, _ = tiktoken.GetEncoding("cl100k_base")
		}
	})
	return encoder
}

// CountMessagesToken 精确计算一组消息的 token 数
// 使用 tiktoken BPE 分词器，跟模型实际计算方式一致
//
// 每条消息的开销包括：
//   - 消息内容的 token 数（精确计算）
//   - 消息格式的固定开销（角色标签、分隔符等，约 4 token/条）
//   - 工具调用的 token 数（函数名 + 参数）
func CountMessagesToken(messages []llm.Message) int {
	enc := getEncoder()
	total := 0

	for _, msg := range messages {
		// 精确计算消息内容的 token 数
		// enc.Encode 把文本拆成 token 列表，len 就是 token 数量
		total += len(enc.Encode(msg.Content, nil, nil))

		// 工具调用的 token 数
		for _, tc := range msg.ToolCalls {
			total += len(enc.Encode(tc.Function.Name, nil, nil))
			total += len(enc.Encode(tc.Function.Arguments, nil, nil))
		}

		// 每条消息的格式开销（OpenAI API 格式的额外 token）
		// 包括 <|start|>{role}\n{content}<|end|> 这些标记
		total += 4
	}

	return total
}

// CountTextToken 精确计算纯文本的 token 数
// 用于计算文件内容、system prompt 等的 token 消耗
func CountTextToken(text string) int {
	enc := getEncoder()
	return len(enc.Encode(text, nil, nil))
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
