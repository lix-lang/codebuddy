// Package llm 实现 LLM 客户端。
//
// 本文件（anthropic.go）实现 Anthropic Messages API 客户端，
// 适用于 Anthropic Claude 以及兼容 Anthropic API 的提供商（如智谱 GLM）。
//
// # Anthropic vs OpenAI API 差异
//
//   - 端点: /v1/messages（OpenAI 用 /chat/completions）
//   - 认证: x-api-key 头（OpenAI 用 Authorization: Bearer）
//   - 必须头: anthropic-version: 2023-06-01
//   - system 消息独立于 messages 数组
//   - 工具调用用 tool_use content block（OpenAI 用 tool_calls 数组）
//   - SSE 事件类型: message_start / content_block_start / content_block_delta / message_delta / message_stop
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// AnthropicClient Anthropic Messages API 客户端
// 适用于 Anthropic Claude 和兼容的提供商（如智谱 GLM 的 Anthropic 端点）
type AnthropicClient struct {
	apiKey         string
	baseURL        string
	model          string
	maxTokens      int
	temperature    float64
	maxContext     int
	supportsVision bool
	httpClient     *http.Client
}

// AnthropicConfig Anthropic 客户端配置
type AnthropicConfig struct {
	APIKey         string
	BaseURL        string
	Model          string
	MaxTokens      int
	Temperature    float64
	MaxContext     int
	SupportsVision bool
}

// NewAnthropicClient 创建 Anthropic 客户端
func NewAnthropicClient(cfg AnthropicConfig) *AnthropicClient {
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.7
	}
	if cfg.MaxContext == 0 {
		cfg.MaxContext = 200000
	}

	return &AnthropicClient{
		apiKey:         cfg.APIKey,
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		model:          cfg.Model,
		maxTokens:      cfg.MaxTokens,
		temperature:    cfg.Temperature,
		maxContext:     cfg.MaxContext,
		supportsVision: cfg.SupportsVision,
		httpClient:     &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *AnthropicClient) ModelName() string     { return c.model }
func (c *AnthropicClient) MaxContextTokens() int { return c.maxContext }
func (c *AnthropicClient) SupportsVision() bool  { return c.supportsVision }

// CountTokens 估算 token 数（Anthropic 没有本地分词器，用字符估算）
func (c *AnthropicClient) CountTokens(messages []Message) (int, error) {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 3
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Name) / 3
			total += len(tc.Function.Arguments) / 3
		}
		total += 4
	}
	return total, nil
}

// ================================================================
// 请求构建
// ================================================================

// anthropicRequest Anthropic Messages API 请求体
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
	Tools     []anthropicToolDef `json:"tools,omitempty"`
}

// anthropicMessage Anthropic 格式的消息
// Content 可以是字符串或 content block 数组
type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string 或 []anthropicContentBlock
}

// anthropicContentBlock 内容块（工具调用用 tool_use，工具结果用 tool_result）
type anthropicContentBlock struct {
	Type  string `json:"type"`            // "text", "tool_use", "tool_result"
	Text  string `json:"text,omitempty"`  // type=text 时
	ID    string `json:"id,omitempty"`    // type=tool_use / tool_result 时
	Name  string `json:"name,omitempty"`  // type=tool_use 时
	Input any    `json:"input,omitempty"` // type=tool_use 时（通常是 map）
	// tool_result 嵌套 content
	Content2 any `json:"content,omitempty"` // type=tool_result 时（字符串或 content block）
}

// anthropicToolDef Anthropic 格式的工具定义
type anthropicToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// buildAnthropicRequest 把通用 Message 列表转换为 Anthropic 请求体
func (c *AnthropicClient) buildAnthropicRequest(messages []Message, tools []ToolDefinition) anthropicRequest {
	var systemPrompt string
	var apiMsgs []anthropicMessage

	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			systemPrompt += msg.Content + "\n"
		case RoleUser:
			apiMsgs = append(apiMsgs, anthropicMessage{
				Role:    "user",
				Content: msg.Content,
			})
		case RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				// assistant 消息带工具调用 → content 是 content block 数组
				var blocks []anthropicContentBlock
				if msg.Content != "" {
					blocks = append(blocks, anthropicContentBlock{
						Type: "text",
						Text: msg.Content,
					})
				}
				for _, tc := range msg.ToolCalls {
					// 把 arguments JSON 字符串解析成 map
					var input any
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						input = map[string]any{}
					}
					blocks = append(blocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: input,
					})
				}
				apiMsgs = append(apiMsgs, anthropicMessage{
					Role:    "assistant",
					Content: blocks,
				})
			} else {
				apiMsgs = append(apiMsgs, anthropicMessage{
					Role:    "assistant",
					Content: msg.Content,
				})
			}
		case RoleTool:
			// tool 结果 → user 消息里嵌 tool_result content block
			apiMsgs = append(apiMsgs, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{
					{
						Type:     "tool_result",
						ID:       msg.ToolCallID,
						Content2: msg.Content,
					},
				},
			})
		}
	}

	// 转换工具定义
	var apiTools []anthropicToolDef
	for _, t := range tools {
		apiTools = append(apiTools, anthropicToolDef{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}

	return anthropicRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    strings.TrimSpace(systemPrompt),
		Messages:  apiMsgs,
		Stream:    true,
		Tools:     apiTools,
	}
}

// ================================================================
// HTTP 请求
// ================================================================

func (c *AnthropicClient) sendAnthropicRequest(ctx context.Context, reqBody anthropicRequest) (*http.Response, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	url := c.baseURL + "/v1/messages"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	log.Printf("[ANTHROPIC] POST %s, model=%s", url, c.model)
	return c.httpClient.Do(req)
}

// ================================================================
// ChatStream 实现 LLMClient 接口
// ================================================================

func (c *AnthropicClient) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition) (<-chan StreamEvent, error) {
	reqBody := c.buildAnthropicRequest(messages, tools)
	log.Printf("[ANTHROPIC] ChatStream start, model=%s, msgs=%d, tools=%d", c.model, len(messages), len(tools))

	resp, err := c.sendAnthropicRequest(ctx, reqBody)
	if err != nil {
		log.Printf("[ANTHROPIC] sendRequest error: %v", err)
		return nil, fmt.Errorf("API 请求失败: %w", err)
	}

	log.Printf("[ANTHROPIC] HTTP %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("[ANTHROPIC] error response: %s", string(body))
		return nil, fmt.Errorf("API 返回错误 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	eventCh := make(chan StreamEvent, 64)
	go func() {
		defer close(eventCh)
		defer resp.Body.Close()
		parseAnthropicSSE(resp.Body, eventCh)
	}()

	return eventCh, nil
}

// ================================================================
// SSE 解析
// ================================================================

// parseAnthropicSSE 解析 Anthropic 格式的 SSE 流
// 事件类型:
//   - event: message_start       → 包含 message 对象（model、usage 等）
//   - event: content_block_start → 新的 content block（text 或 tool_use）
//   - event: content_block_delta → 内容增量（text delta 或 input_json_delta）
//   - event: content_block_stop  → 当前 block 结束
//   - event: message_delta       → message 级别的增量（stop_reason、usage）
//   - event: message_stop        → 流结束
func parseAnthropicSSE(body io.Reader, eventCh chan<- StreamEvent) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0), 1024*1024)

	// 工具调用拼接缓冲区
	toolBuffers := make(map[int]*toolCallBuffer) // key = content_block index
	var currentEventType string
	lineCount := 0
	var inputTokens int  // 从 message_start 累积
	var outputTokens int // 从 message_delta 累积

	log.Printf("[ANTHROPIC-SSE] start reading stream")

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++

		if lineCount <= 5 {
			log.Printf("[ANTHROPIC-SSE] line %d: %q", lineCount, line)
		}

		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		switch currentEventType {
		case "message_start":
			inputTokens = extractInputTokens(data)

		case "content_block_start":
			handleContentBlockStart(data, toolBuffers)

		case "content_block_delta":
			handleContentBlockDelta(data, toolBuffers, eventCh)

		case "message_delta":
			outputTokens += extractOutputTokens(data)

		case "message_stop":
			total := inputTokens + outputTokens
			log.Printf("[ANTHROPIC-SSE] message_stop, input=%d output=%d total=%d", inputTokens, outputTokens, total)
			eventCh <- StreamEvent{
				Type: StreamEventDone,
				Usage: &Usage{
					PromptTokens:     inputTokens,
					CompletionTokens: outputTokens,
					TotalTokens:      total,
				},
			}
			return
		}

		currentEventType = ""
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[ANTHROPIC-SSE] scanner error: %v", err)
	}
	log.Printf("[ANTHROPIC-SSE] stream ended without message_stop, totalLines=%d", lineCount)
}

// extractInputTokens 从 message_start 提取 input_tokens
func extractInputTokens(data string) int {
	var msg struct {
		Message struct {
			Usage struct {
				InputTokens int `json:"input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return 0
	}
	return msg.Message.Usage.InputTokens
}

// extractOutputTokens 从 message_delta 提取 output_tokens
func extractOutputTokens(data string) int {
	var msg struct {
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return 0
	}
	return msg.Usage.OutputTokens
}

// handleContentBlockStart 处理 content_block_start 事件
func handleContentBlockStart(data string, toolBuffers map[int]*toolCallBuffer) {
	var block struct {
		Type         string `json:"type"`
		Index        int    `json:"index"`
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id,omitempty"`
			Name string `json:"name,omitempty"`
			Text string `json:"text,omitempty"`
		} `json:"content_block"`
	}
	if err := json.Unmarshal([]byte(data), &block); err != nil {
		log.Printf("[ANTHROPIC-SSE] parse content_block_start error: %v", err)
		return
	}

	if block.ContentBlock.Type == "tool_use" {
		toolBuffers[block.Index] = &toolCallBuffer{
			id:   block.ContentBlock.ID,
			name: block.ContentBlock.Name,
		}
		log.Printf("[ANTHROPIC-SSE] tool_use block start: id=%s name=%s", block.ContentBlock.ID, block.ContentBlock.Name)
	}
}

// handleContentBlockDelta 处理 content_block_delta 事件
func handleContentBlockDelta(data string, toolBuffers map[int]*toolCallBuffer, eventCh chan<- StreamEvent) {
	var delta struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text,omitempty"`
			PartialJSON string `json:"partial_json,omitempty"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &delta); err != nil {
		log.Printf("[ANTHROPIC-SSE] parse content_block_delta error: %v", err)
		return
	}

	switch delta.Delta.Type {
	case "text_delta":
		if delta.Delta.Text != "" {
			eventCh <- StreamEvent{
				Type:    StreamEventContent,
				Content: delta.Delta.Text,
			}
		}

	case "input_json_delta":
		// 工具调用参数增量
		buf, ok := toolBuffers[delta.Index]
		if ok {
			buf.arguments += delta.Delta.PartialJSON
			eventCh <- StreamEvent{
				Type: StreamEventToolCall,
				ToolCalls: &ToolCallChunk{
					ID:             buf.id,
					FunctionName:   buf.name,
					ArgumentsDelta: delta.Delta.PartialJSON,
				},
			}
		}
	}
}
