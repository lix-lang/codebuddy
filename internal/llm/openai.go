// Package llm 实现 LLM 客户端，负责跟大模型 API 通信。
//
// 本文件（openai.go）实现 OpenAI 兼容的 LLM 客户端，适用于所有兼容 OpenAI API 格式的提供商：
//   - GLM（智谱）: base_url = "https://open.bigmodel.cn/api/paas/v4"
//   - DeepSeek:    base_url = "https://api.deepseek.com/v1"
//   - OpenAI:      base_url = "https://api.openai.com/v1"
//   - Ollama 本地: base_url = "http://localhost:11434/v1"
//
// # 整体调用流程
//
// Agent 调用 ChatStream() 时的完整流程：
//
//  1. 组装请求体（buildRequestBody）：
//     把 Message 列表 + ToolDefinition 列表 → 转成 JSON 请求体
//     请求体结构：{model, messages, stream: true, max_tokens, temperature, tools}
//
//  2. 发送 HTTP 请求（sendRequest）：
//     POST 到 base_url + "/chat/completions"
//     请求头带 Authorization: Bearer <api_key>
//     返回 HTTP 响应（状态码 200 才继续）
//
//  3. 创建 channel + 启动 goroutine 读取 SSE 流：
//     创建一个缓冲大小 64 的 channel（eventCh）
//     启动一个 goroutine（后台线程）专门读取 API 的流式响应
//     立刻把 eventCh 返回给 Agent，Agent 从 channel 里读事件
//
//  4. 解析 SSE 流（parseSSEStream）：
//     SSE 格式是逐行传输的，每行格式：data: <JSON>
//     逐行读取，去掉 "data: " 前缀，解析 JSON
//     遇到 "data: [DONE]" 表示流结束
//
//  5. 解析每个 chunk（parseSSEChunk）：
//     每个 chunk 可能是三种类型之一：
//     - 文字内容（delta.content）→ 推出 StreamEventContent
//     - 工具调用碎片（delta.tool_calls）→ 拼接后推出 StreamEventToolCall
//     - token 用量（usage）→ 推出 StreamEventDone
//
//  6. 工具调用碎片拼接（handleToolCallChunk）：
//     一个工具调用的参数可能分成多个 chunk 传输：
//       chunk1: arguments = "{\"pa"
//       chunk2: arguments = "th\":\"main"
//       chunk3: arguments = ".go\"}"
//     用 toolCallBuffer 按 index 拼接：最终得到 {"path":"main.go"}
//
// # 并发模型
//
// goroutine（后台线程）负责读 SSE 流 → 推入 channel
// Agent（前台主循环）从 channel 读 → 显示/处理
// 两个线程通过 channel 传递数据，互不阻塞
//
// # 取消机制
//
// context 从 Agent → ChatStream → HTTP 请求，层层传递
// 用户按 Ctrl+C → context 取消 → HTTP 请求中断 → goroutine 退出 → channel 关闭
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkoukk/tiktoken-go"
)

// OpenAIClient OpenAI 兼容的 LLM 客户端
// 适用于 OpenAI / GLM / DeepSeek / Ollama 等兼容 OpenAI API 格式的提供商
type OpenAIClient struct {
	apiKey         string // API Key
	baseURL        string // API 地址，如 "https://open.bigmodel.cn/api/paas/v4"
	model          string // 模型名，如 "glm-4"
	maxTokens      int    // 最大输出 token
	temperature    float64      // 生成温度，控制输出随机性
	maxContext     int          // 最大上下文窗口（token）
	supportsVision bool         // 是否支持图片
	httpClient     *http.Client // 底层 HTTP 客户端，带 5 分钟超时
}

// OpenAIConfig OpenAI 客户端配置
type OpenAIConfig struct {
	APIKey         string  // API 密钥，用于 Authorization: Bearer 认证
	BaseURL        string  // API 基础地址，如 "https://open.bigmodel.cn/api/paas/v4"
	Model          string  // 模型名称，如 "glm-4"、"gpt-4o"
	MaxTokens      int     // 单次请求最大输出 token 数（默认 4096）
	Temperature    float64 // 生成温度，0~2，越高越随机（默认 0.7）
	MaxContext     int     // 模型最大上下文窗口 token 数（默认 128000）
	SupportsVision bool    // 模型是否支持图片/视觉输入
}

// NewOpenAIClient 创建 OpenAI 兼容客户端
func NewOpenAIClient(cfg OpenAIConfig) *OpenAIClient {
	// 设置默认值
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.7
	}
	if cfg.MaxContext == 0 {
		cfg.MaxContext = 128000
	}

	return &OpenAIClient{
		apiKey:         cfg.APIKey,
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		model:          cfg.Model,
		maxTokens:      cfg.MaxTokens,
		temperature:    cfg.Temperature,
		maxContext:     cfg.MaxContext,
		supportsVision: cfg.SupportsVision,
		// http.Client 是 Go 的 HTTP 客户端
		// Timeout 设置整体请求超时时间（5 分钟，因为 LLM 响应可能很慢）
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// ModelName 返回当前模型名（实现 LLMClient 接口）
func (c *OpenAIClient) ModelName() string {
	return c.model
}

// MaxContextTokens 返回模型最大上下文窗口（实现 LLMClient 接口）
func (c *OpenAIClient) MaxContextTokens() int {
	return c.maxContext
}

// SupportsVision 返回模型是否支持图片（实现 LLMClient 接口）
func (c *OpenAIClient) SupportsVision() bool {
	return c.supportsVision
}

// CountTokens 精确计算消息消耗的 token 数（实现 LLMClient 接口）
// 使用 tiktoken BPE 分词器，跟模型实际计算方式一致
func (c *OpenAIClient) CountTokens(messages []Message) (int, error) {
	enc, err := tiktoken.EncodingForModel(c.model)
	if err != nil {
		// 模型名不认识，回退到 cl100k_base（GPT-4 编码，大多数兼容模型也适用）
		enc, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return 0, fmt.Errorf("获取 tiktoken 编码器失败: %w", err)
		}
	}

	total := 0
	for _, msg := range messages {
		// 精确计算消息内容的 token 数
		total += len(enc.Encode(msg.Content, nil, nil))
		for _, tc := range msg.ToolCalls {
			total += len(enc.Encode(tc.Function.Name, nil, nil))
			total += len(enc.Encode(tc.Function.Arguments, nil, nil))
		}
		// 每条消息的格式开销
		total += 4
	}
	return total, nil
}

// ChatStream 流式调用 LLM（实现 LLMClient 接口）
func (c *OpenAIClient) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition) (<-chan StreamEvent, error) {
	// 1. 组装请求体
	reqBody := c.buildRequestBody(messages, tools)

	// 2. 发送 HTTP 请求
	resp, err := c.sendRequest(ctx, reqBody)
	if err != nil {
		return nil, fmt.Errorf("API 请求失败: %w", err)
	}

	// 3. 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API 返回错误 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// 4. 创建 channel，启动 goroutine 读取 SSE 流
	// eventCh 缓冲大小 64，避免 goroutine 因消费者慢而阻塞
	eventCh := make(chan StreamEvent, 64)

	// go 关键字启动一个 goroutine（轻量级线程，并发执行）
	go func() {
		// defer 确保函数结束时关闭 channel 和 response body
		defer close(eventCh)
		defer resp.Body.Close()

		// parseSSEStream 从 HTTP 响应体中读取 SSE 流，解析后推入 channel
		parseSSEStream(resp.Body, eventCh)
	}()

	return eventCh, nil
}

// buildRequestBody 组装发往 API 的 JSON 请求体
func (c *OpenAIClient) buildRequestBody(messages []Message, tools []ToolDefinition) map[string]any {
	// 把 Message 转成 API 要求的格式
	// apiMessages 存放转换后的每条消息，格式为 map[string]any 以兼容 JSON 序列化
	var apiMessages []map[string]any
	for _, msg := range messages {
		// m 是单条消息的 map 表示，包含 role 和 content
		m := map[string]any{
			"role":    string(msg.Role),
			"content": msg.Content,
		}
		// 只有 assistant 消息才有 tool_calls
		if len(msg.ToolCalls) > 0 {
			m["tool_calls"] = msg.ToolCalls
		}
		// 只有 tool 消息才有 tool_call_id 和 name
		if msg.ToolCallID != "" {
			m["tool_call_id"] = msg.ToolCallID
		}
		if msg.Name != "" {
			m["name"] = msg.Name
		}
		apiMessages = append(apiMessages, m)
	}

	// body 是发往 API 的完整请求体
	body := map[string]any{
		"model":       c.model,
		"messages":    apiMessages,
		"stream":      true, // 启用 SSE 流式响应
		"max_tokens":  c.maxTokens,
		"temperature": c.temperature,
	}

	// 如果有工具定义，加入 tools 参数
	if len(tools) > 0 {
		body["tools"] = tools
	}

	return body
}

// sendRequest 发送 HTTP POST 请求到 API
func (c *OpenAIClient) sendRequest(ctx context.Context, body map[string]any) (*http.Response, error) {
	// jsonBody 是序列化后的 JSON 请求体字节流
	// json.Marshal 把 map 转成 JSON 字节流
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// 拼接完整的 API 地址
	// 比如 baseURL="https://open.bigmodel.cn/api/paas/v4" → "https://open.bigmodel.cn/api/paas/v4/chat/completions"
	url := c.baseURL + "/chat/completions"

	// http.NewRequestWithContext 创建带 context 的 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	// Authorization: Bearer <api_key> 是 OpenAI API 的标准认证方式
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// c.httpClient.Do 发送请求并返回响应
	return c.httpClient.Do(req)
}

// parseSSEStream 从 HTTP 响应体中读取 SSE 流
// SSE (Server-Sent Events) 格式：
//   data: {"choices":[{"delta":{"content":"你"}}]}
//   data: {"choices":[{"delta":{"content":"好"}}]}
//   data: [DONE]
func parseSSEStream(body io.Reader, eventCh chan<- StreamEvent) {
	// bufio.NewScanner 按行读取（SSE 是一行一行传的）
	// scanner 是按行分割的读取器，每次 Scan() 读一行
	scanner := bufio.NewScanner(body)
	// 设置缓冲区大小为 1MB（有些 chunk 很大）
	scanner.Buffer(make([]byte, 0), 1024*1024)

	// toolCallBuffers 用来拼接工具调用碎片
	// key 是工具调用 ID，value 是拼接后的结果
	toolCallBuffers := make(map[string]*toolCallBuffer)

	for scanner.Scan() {
		// line 是从 SSE 流中读到的当前行文本
		line := scanner.Text()

		// SSE 格式中，每行以 "data: " 开头
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		// 去掉 "data: " 前缀，拿到 JSON 内容
		// data 是去掉 SSE 前缀后的原始 JSON 字符串
		data := strings.TrimPrefix(line, "data: ")

		// [DONE] 表示流结束
		if data == "[DONE]" {
			eventCh <- StreamEvent{Type: StreamEventDone}
			return
		}

		// 解析 JSON chunk
		event, err := parseSSEChunk(data, toolCallBuffers)
		if err != nil {
			continue // 跳过无法解析的 chunk
		}

		if event != nil {
			eventCh <- *event
		}
	}
}

// toolCallBuffer 用于拼接同一个工具调用的多个碎片
type toolCallBuffer struct {
	id        string // 工具调用 ID，所有碎片共享同一个 ID
	name      string // 工具名称（仅第一个碎片携带）
	arguments string // 已拼接的参数 JSON 片段
}

// parseSSEChunk 解析单个 SSE chunk 的 JSON
func parseSSEChunk(data string, buffers map[string]*toolCallBuffer) (*StreamEvent, error) {
	// chunk 的 JSON 结构（简化）：
	// {"choices":[{"delta":{"content":"你好"},"index":0}]}
	// {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_xxx","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}
	var chunk struct {
		// Choices 是模型返回的候选列表，通常只有一个元素
		Choices []struct {
			// Delta 是本次增量更新的内容（与上一块拼接才是完整回复）
			Delta struct {
				// Content 是本次新增的文本片段
				Content string `json:"content"`
				// ToolCalls 是本次新增的工具调用碎片
				ToolCalls []struct {
					// Index 是工具调用的序号，同一轮中唯一标识一个调用
					Index int `json:"index"`
					// ID 是工具调用 ID，仅第一块携带
					ID string `json:"id"`
					// Function 是工具调用的函数信息
					Function struct {
						// Name 是工具名称，仅第一块携带
						Name string `json:"name"`
						// Arguments 是参数 JSON 的增量片段
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		// Usage 是 token 用量统计，通常在最后一个 chunk 或 [DONE] 前携带
		Usage *Usage `json:"usage"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, err
	}

	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	// choice 取第一个候选结果（通常 API 只返回一个）
	choice := chunk.Choices[0]

	// 1. 文字内容
	if choice.Delta.Content != "" {
		return &StreamEvent{
			Type:    StreamEventContent,
			Content: choice.Delta.Content,
		}, nil
	}

	// 2. 工具调用碎片
	if len(choice.Delta.ToolCalls) > 0 {
		// tc 是本次增量中的第一个工具调用碎片
		tc := choice.Delta.ToolCalls[0]
		return handleToolCallChunk(tc, buffers), nil
	}

	// 3. token 用量（某些 API 在最后一个 chunk 带上 usage）
	if chunk.Usage != nil {
		return &StreamEvent{
			Type:  StreamEventDone,
			Usage: chunk.Usage,
		}, nil
	}

	return nil, nil
}

// handleToolCallChunk 处理工具调用碎片，拼接成完整的调用
func handleToolCallChunk(tc struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}, buffers map[string]*toolCallBuffer) *StreamEvent {
	// 用 index 作为 key（同一轮调用中，index 唯一标识一个工具调用）
	// key 是 buffer 映射中的查找键，由工具调用序号转成字符串
	key := fmt.Sprintf("%d", tc.Index)

	// buf 是该工具调用的拼接缓冲区，不存在则新建
	buf, exists := buffers[key]
	if !exists {
		// 第一个碎片：创建新的 buffer
		buf = &toolCallBuffer{
			id:   tc.ID,
			name: tc.Function.Name,
		}
		buffers[key] = buf
	}

	// 拼接参数碎片
	buf.arguments += tc.Function.Arguments

	// 每个碎片都推出去，Agent 可以选择是否实时显示
	return &StreamEvent{
		Type: StreamEventToolCall,
		ToolCalls: &ToolCallChunk{
			ID:             buf.id,
			FunctionName:   buf.name,
			ArgumentsDelta: tc.Function.Arguments,
		},
	}
}
