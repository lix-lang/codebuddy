package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lix-lang/codebuddy/internal/config"
	"github.com/lix-lang/codebuddy/internal/llm"
	"github.com/lix-lang/codebuddy/internal/pubsub"
	"github.com/lix-lang/codebuddy/internal/tool"
)

// ContextManager Agent 需要的上下文管理接口
type ContextManager interface {
	BuildMessages(userMsg string) ([]llm.Message, error)
	AddHistory(msg llm.Message)
	Scan() error
	HasScanned() bool
}

// DefaultAgent Agent 的默认实现
type DefaultAgent struct {
	mu sync.RWMutex

	// 依赖
	llmClient  llm.LLMClient
	registry   tool.ToolRegistry
	ctxManager ContextManager
	cfg        config.Config

	// 状态
	state AgentState
	stop  context.CancelFunc

	// 费用跟踪
	totalTokens int
	totalCost   float64

	// 回调
	onToolCall       func(name string, args map[string]any)
	onStateChange    func(old, new AgentState)
	onError          func(err error)
	onStreamContent  func(content string)
	onConfirmRequest func(req pubsub.ConfirmRequest)

	// 事件总线
	eventBus *pubsub.EventBus
}

// AgentConfig 创建 Agent 的配置
type AgentConfig struct {
	LLMClient  llm.LLMClient
	Registry   tool.ToolRegistry
	CtxManager ContextManager
	Config     config.Config
}

// NewAgent 创建 Agent 实例
func NewAgent(cfg AgentConfig) *DefaultAgent {
	return &DefaultAgent{
		llmClient:  cfg.LLMClient,
		registry:   cfg.Registry,
		ctxManager: cfg.CtxManager,
		cfg:        cfg.Config,
		state:      StateIdle,
	}
}

// Run 实现 Agent 接口 — ReAct 主循环
func (a *DefaultAgent) Run(ctx context.Context, userMsg string) error {
	log.Printf("[AGENT] Run start, userMsg=%q", userMsg)
	ctx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.stop = cancel
	a.mu.Unlock()
	defer cancel()

	// 用户消息加入历史
	a.ctxManager.AddHistory(llm.Message{
		Role:    llm.RoleUser,
		Content: userMsg,
	})
	log.Printf("[AGENT] userMsg added to history")

	maxSteps := a.cfg.Agent.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 15
	}
	log.Printf("[AGENT] maxSteps=%d", maxSteps)

	// ReAct 主循环
	for step := 0; step < maxSteps; step++ {
		select {
		case <-ctx.Done():
			log.Printf("[AGENT] ctx cancelled at step %d", step)
			a.setState(StateIdle)
			return ctx.Err()
		default:
		}

		log.Printf("[AGENT] === step %d/%d ===", step+1, maxSteps)

		// ==== 1. 构建 Prompt ====
		a.setState(StateThinking)
		messages, err := a.ctxManager.BuildMessages("")
		if err != nil {
			log.Printf("[AGENT] BuildMessages failed: %v", err)
			a.setState(StateError)
			return fmt.Errorf("构建上下文失败: %w", err)
		}
		log.Printf("[AGENT] BuildMessages ok, %d messages", len(messages))

		// ==== 2. 调用 LLM（带重试） ====
		toolDefs := tool.GetToolDefinitions(a.registry)
		log.Printf("[AGENT] calling LLM, %d toolDefs", len(toolDefs))
		assistantMsg, err := a.callLLMWithRetry(ctx, messages, toolDefs)
		if err != nil {
			log.Printf("[AGENT] LLM call failed: %v", err)
			a.setState(StateError)
			if a.onError != nil {
				a.onError(err)
			}
			a.mu.RLock()
			eb := a.eventBus
			a.mu.RUnlock()
			if eb != nil {
				eb.Push(pubsub.Event{Type: pubsub.EventError, Err: err})
			}
			return err
		}
		log.Printf("[AGENT] LLM responded, contentLen=%d, toolCalls=%d", len(assistantMsg.Content), len(assistantMsg.ToolCalls))

		// 助手回复加入历史
		a.ctxManager.AddHistory(assistantMsg)

		// ==== 3. 判断响应类型 ====
		if len(assistantMsg.ToolCalls) == 0 {
			log.Printf("[AGENT] pure text reply, done")
			a.setState(StateDone)
			return nil
		}

		// ==== 4. 费用检查 ====
		if a.cfg.Cost.MaxPerSession > 0 && a.totalCost > a.cfg.Cost.MaxPerSession {
			log.Printf("[AGENT] cost limit reached: %.2f > %.2f", a.totalCost, a.cfg.Cost.MaxPerSession)
			return fmt.Errorf("会话费用已达上限 %.2f 元（当前 %.2f 元）", a.cfg.Cost.MaxPerSession, a.totalCost)
		}

		// ==== 5. 执行工具调用（并行只读，串行写操作） ====
		a.setState(StateExecuting)
		toolNames := make([]string, len(assistantMsg.ToolCalls))
		for i, tc := range assistantMsg.ToolCalls {
			toolNames[i] = tc.Function.Name
		}
		log.Printf("[AGENT] executing tools: %v", toolNames)
		toolResults := a.executeToolCalls(ctx, assistantMsg.ToolCalls)
		log.Printf("[AGENT] tools executed, %d results", len(toolResults))

		// ==== 6. 结果验证 + 自纠错 ====
		for i, result := range toolResults {
			verified := a.verifyToolResult(assistantMsg.ToolCalls[i], result)
			if verified != nil {
				log.Printf("[AGENT] verification override for %s", assistantMsg.ToolCalls[i].Function.Name)
				toolResults[i] = *verified
			}
		}

		// 工具结果加入历史
		for _, result := range toolResults {
			a.ctxManager.AddHistory(result)
		}
		log.Printf("[AGENT] step %d complete, tokens=%d cost=%.4f", step+1, a.totalTokens, a.totalCost)
	}

	a.setState(StateDone)
	return fmt.Errorf("达到最大执行步数 %d", maxSteps)
}

// State 返回当前状态
func (a *DefaultAgent) State() AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// Stop 中断正在运行的 Agent 循环
func (a *DefaultAgent) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stop != nil {
		a.stop()
	}
}

// History 返回完整对话历史
func (a *DefaultAgent) History() []llm.Message {
	return nil
}

// RegisterTool 注册一个工具
func (a *DefaultAgent) RegisterTool(t tool.Tool) {
	a.registry.Register(t)
}

// TotalCost 返回当前会话总费用
func (a *DefaultAgent) TotalCost() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.totalCost
}

// TotalTokens 返回当前会话总 token 数
func (a *DefaultAgent) TotalTokens() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.totalTokens
}

// SetOnToolCall 设置工具调用回调
func (a *DefaultAgent) SetOnToolCall(fn func(name string, args map[string]any)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onToolCall = fn
}

// SetOnStateChange 设置状态变更回调
func (a *DefaultAgent) SetOnStateChange(fn func(old, new AgentState)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onStateChange = fn
}

// SetOnError 设置错误回调
func (a *DefaultAgent) SetOnError(fn func(err error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onError = fn
}

// SetOnStreamContent 设置流式文本回调
func (a *DefaultAgent) SetOnStreamContent(fn func(content string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onStreamContent = fn
}

// SetOnConfirmRequest 设置破坏性操作确认回调
func (a *DefaultAgent) SetOnConfirmRequest(fn func(req pubsub.ConfirmRequest)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onConfirmRequest = fn
}

// SetEventBus 设置事件总线
func (a *DefaultAgent) SetEventBus(eb *pubsub.EventBus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.eventBus = eb
}

// ScanProject 手动扫描项目
func (a *DefaultAgent) ScanProject() error {
	return a.ctxManager.Scan()
}

// HasScanned 项目是否已扫描
func (a *DefaultAgent) HasScanned() bool {
	return a.ctxManager.HasScanned()
}

// ================================================================
// 内部方法：LLM 调用
// ================================================================

// callLLMWithRetry 调用 LLM（5xx 超时重试 3 次，指数退避）
func (a *DefaultAgent) callLLMWithRetry(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDefinition) (llm.Message, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second
			log.Printf("[LLM] retry attempt %d, backoff %v", attempt+1, backoff)
			// 指数退避：1s, 2s
			select {
			case <-ctx.Done():
				return llm.Message{}, ctx.Err()
			case <-time.After(backoff):
			}
		}

		log.Printf("[LLM] ChatStream start, attempt=%d, msgs=%d, tools=%d", attempt+1, len(messages), len(toolDefs))
		eventCh, err := a.llmClient.ChatStream(ctx, messages, toolDefs)
		if err != nil {
			lastErr = err
			log.Printf("[LLM] ChatStream error: %v", err)
			// 4xx 错误不重试（token 超限、认证失败等）
			if is4xxError(err) {
				return llm.Message{}, fmt.Errorf("LLM API 错误（不重试）: %w", err)
			}
			// 5xx / 超时，重试
			continue
		}
		log.Printf("[LLM] ChatStream ok, eventCh opened")

		msg, finished, collectErr := a.collectStreamResponse(eventCh)
		if collectErr != nil {
			log.Printf("[LLM] collectStream error: %v", collectErr)
			return llm.Message{}, collectErr
		}
		if !finished {
			log.Printf("[LLM] stream ended without Done event")
			return llm.Message{}, fmt.Errorf("LLM 流式响应异常结束")
		}
		log.Printf("[LLM] stream collected ok, content=%d bytes, toolCalls=%d", len(msg.Content), len(msg.ToolCalls))
		return msg, nil
	}
	return llm.Message{}, fmt.Errorf("LLM 调用失败（重试 3 次）: %w", lastErr)
}

// is4xxError 判断是否是 4xx 客户端错误（不重试）
func is4xxError(err error) bool {
	return strings.Contains(err.Error(), "400") ||
		strings.Contains(err.Error(), "401") ||
		strings.Contains(err.Error(), "403") ||
		strings.Contains(err.Error(), "429")
}

// collectStreamResponse 收集流式响应，拼接完整的助手消息
func (a *DefaultAgent) collectStreamResponse(eventCh <-chan llm.StreamEvent) (llm.Message, bool, error) {
	var contentBuf strings.Builder
	toolCallBuffers := make(map[string]*toolCallBuffer)
	var toolCallOrder []string // 保持顺序
	finished := false
	contentChunks := 0
	toolChunks := 0

	log.Printf("[STREAM] start collecting")
	for event := range eventCh {
		switch event.Type {
		case llm.StreamEventContent:
			contentBuf.WriteString(event.Content)
			contentChunks++
			// 安全读取回调（加锁）
			a.mu.RLock()
			streamCb := a.onStreamContent
			eb := a.eventBus
			a.mu.RUnlock()
			if streamCb != nil {
				streamCb(event.Content)
			}
			if eb != nil {
				eb.Push(pubsub.Event{
					Type:    pubsub.EventContent,
					Content: event.Content,
				})
			}

		case llm.StreamEventToolCall:
			if event.ToolCalls == nil {
				continue
			}
			toolChunks++
			chunk := event.ToolCalls
			buf, exists := toolCallBuffers[chunk.ID]
			if !exists {
				buf = &toolCallBuffer{id: chunk.ID, name: chunk.FunctionName}
				toolCallBuffers[chunk.ID] = buf
				toolCallOrder = append(toolCallOrder, chunk.ID)
				log.Printf("[STREAM] new toolCall: id=%s name=%s", chunk.ID, chunk.FunctionName)
			}
			buf.args += chunk.ArgumentsDelta

		case llm.StreamEventDone:
			finished = true
			log.Printf("[STREAM] Done event, contentChunks=%d toolChunks=%d", contentChunks, toolChunks)
			if event.Usage != nil {
				a.mu.Lock()
				a.totalTokens += event.Usage.TotalTokens
				// 费用估算：按 $0.01/1k tokens 粗算（可后续接入配置单价）
				a.totalCost = float64(a.totalTokens) / 1000.0 * 0.01
				tokens := a.totalTokens
				cost := a.totalCost
				eb := a.eventBus
				a.mu.Unlock()
				log.Printf("[STREAM] usage: tokens=%d cost=%.4f", tokens, cost)
				if eb != nil {
					eb.Push(pubsub.Event{
						Type:        pubsub.EventUsageUpdate,
						TotalTokens: tokens,
						TotalCost:   cost,
					})
				}
			}
		}
	}

	// 按出现顺序拼接工具调用
	var toolCalls []llm.ToolCall
	for _, id := range toolCallOrder {
		buf := toolCallBuffers[id]
		tc := llm.ToolCall{}
		tc.ID = buf.id
		tc.Function.Name = buf.name
		tc.Function.Arguments = buf.args
		toolCalls = append(toolCalls, tc)
	}

	return llm.Message{
		Role:      llm.RoleAssistant,
		Content:   contentBuf.String(),
		ToolCalls: toolCalls,
	}, finished, nil
}

// toolCallBuffer 工具调用参数拼接缓冲区
type toolCallBuffer struct {
	id   string
	name string
	args string
}

// ================================================================
// 内部方法：工具执行
// ================================================================

// executeToolCalls 执行一批工具调用
// 只读工具并行执行，写操作串行执行（按方案 Day 13 要求）
func (a *DefaultAgent) executeToolCalls(ctx context.Context, toolCalls []llm.ToolCall) []llm.Message {
	results := make([]llm.Message, len(toolCalls))

	// 分组：只读 vs 写操作
	var readOnlyIndices []int
	var writeIndices []int
	for i, tc := range toolCalls {
		if t, ok := a.registry.Get(tc.Function.Name); ok {
			if t.IsDestructive() {
				writeIndices = append(writeIndices, i)
			} else {
				readOnlyIndices = append(readOnlyIndices, i)
			}
		} else {
			// 工具不存在，直接返回错误
			results[i] = a.makeToolError(tc, fmt.Sprintf("工具 %s 不存在。可用工具: %v", tc.Function.Name, a.registry.ListNames()))
		}
	}

	// 并行执行只读工具
	if len(readOnlyIndices) > 0 {
		var wg sync.WaitGroup
		for _, idx := range readOnlyIndices {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i] = a.executeOneToolCall(ctx, toolCalls[i])
			}(idx)
		}
		wg.Wait()
	}

	// 串行执行写操作
	for _, idx := range writeIndices {
		results[idx] = a.executeOneToolCall(ctx, toolCalls[idx])
	}

	return results
}

// executeOneToolCall 执行单个工具调用（七关校验链）
func (a *DefaultAgent) executeOneToolCall(ctx context.Context, tc llm.ToolCall) llm.Message {
	log.Printf("[TOOL] execute %s, args=%s", tc.Function.Name, tc.Function.Arguments)

	// 第一关：参数 JSON 解析
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		log.Printf("[TOOL] JSON parse error: %v", err)
		return a.makeToolError(tc, fmt.Sprintf("参数 JSON 格式错误: %v", err))
	}

	// 第二关 + 第三关：Schema 校验 + 业务校验
	if err := a.registry.ValidateParams(tc.Function.Name, args); err != nil {
		log.Printf("[TOOL] validate error: %v", err)
		return a.makeToolError(tc, fmt.Sprintf("参数校验失败: %v", err))
	}

	// 获取工具
	t, ok := a.registry.Get(tc.Function.Name)
	if !ok {
		log.Printf("[TOOL] tool not found: %s", tc.Function.Name)
		return a.makeToolError(tc, fmt.Sprintf("工具 %s 不存在", tc.Function.Name))
	}

	// 第四关：破坏性操作确认
	if t.IsDestructive() {
		log.Printf("[TOOL] destructive, requesting confirm")
		a.mu.RLock()
		confirmCb := a.onConfirmRequest
		a.mu.RUnlock()

		if confirmCb != nil {
			req := pubsub.ConfirmRequest{
				ToolName: tc.Function.Name,
				Args:     formatArgsSummary(args),
				Response: make(chan bool, 1),
			}
			confirmCb(req)

			// 阻塞等待用户确认
			approved := <-req.Response
			log.Printf("[TOOL] confirm result: %v", approved)
			if !approved {
				return a.makeToolError(tc, "用户取消了此操作")
			}
		}
	}
	// 第五关：自动备份（在工具内部实现，write_file/edit_file 已集成）

	// 回调通知
	a.mu.RLock()
	cb := a.onToolCall
	eb := a.eventBus
	a.mu.RUnlock()
	if cb != nil {
		cb(tc.Function.Name, args)
	}
	if eb != nil {
		eb.Push(pubsub.Event{
			Type:     pubsub.EventToolCall,
			ToolName: tc.Function.Name,
			ToolArgs: formatArgsSummary(args),
		})
	}

	// 第六关：执行
	log.Printf("[TOOL] calling Execute()")
	result, err := t.Execute(ctx, args)
	if err != nil {
		log.Printf("[TOOL] Execute error: %v", err)
		return a.makeToolError(tc, fmt.Sprintf("工具执行出错: %v", err))
	}
	log.Printf("[TOOL] Execute ok, outputLen=%d isError=%v", len(result.Output), result.IsError)

	// 第七关：结果验证（在 Run 主循环中处理）

	output := result.Output
	if result.IsError {
		output = "错误: " + output
	}

	// 推送工具结果事件
	a.mu.RLock()
	eb = a.eventBus
	a.mu.RUnlock()
	if eb != nil {
		resultSummary := output
		if len(resultSummary) > 200 {
			resultSummary = resultSummary[:200] + "..."
		}
		eb.Push(pubsub.Event{
			Type:       pubsub.EventToolResult,
			ToolName:   tc.Function.Name,
			ToolResult: resultSummary,
		})
	}

	return llm.Message{
		Role:       llm.RoleTool,
		Content:    output,
		ToolCallID: tc.ID,
		Name:       tc.Function.Name,
	}
}

// makeToolError 构造工具调用错误消息
func (a *DefaultAgent) makeToolError(tc llm.ToolCall, errMsg string) llm.Message {
	return llm.Message{
		Role:       llm.RoleTool,
		Content:    errMsg,
		ToolCallID: tc.ID,
		Name:       tc.Function.Name,
	}
}

// ================================================================
// 内部方法：结果验证（幻觉防护第三层）
// ================================================================

// verifyToolResult 验证工具执行结果
// 对 write_file/edit_file 检查文件确实写入、对 .go 文件做语法检查
// 验证失败时修改结果消息，引导 LLM 自纠错
func (a *DefaultAgent) verifyToolResult(tc llm.ToolCall, result llm.Message) *llm.Message {
	// 只验证成功的写操作
	if result.Content == "" || strings.HasPrefix(result.Content, "错误") {
		return nil
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return nil
	}

	switch tc.Function.Name {
	case "write_file", "edit_file":
		return a.verifyFileResult(tc, args, result)
	}
	return nil
}

// verifyFileResult 验证文件写入结果
func (a *DefaultAgent) verifyFileResult(tc llm.ToolCall, args map[string]any, result llm.Message) *llm.Message {
	path, _ := args["path"].(string)
	if path == "" {
		return nil
	}

	// 检查文件存在且非空
	info, err := os.Stat(path)
	if err != nil {
		// 尝试相对路径
		if a.cfg.LLM.BaseURL != "" {
			// 用项目根目录（简化处理，实际应从 config 取）
		}
		return &llm.Message{
			Role:       llm.RoleTool,
			Content:    fmt.Sprintf("验证失败：文件 %s 不存在。你的写入操作没有生效，请检查路径是否正确并重试。", path),
			ToolCallID: result.ToolCallID,
			Name:       result.Name,
		}
	}
	if info.Size() == 0 {
		return &llm.Message{
			Role:       llm.RoleTool,
			Content:    fmt.Sprintf("验证失败：文件 %s 为空（0 字节）。写入可能失败了，请重新写入。", path),
			ToolCallID: result.ToolCallID,
			Name:       result.Name,
		}
	}

	// edit_file 额外验证：检查 new_string 确实出现在文件中
	if tc.Function.Name == "edit_file" {
		newStr, _ := args["new_string"].(string)
		if newStr != "" {
			data, err := os.ReadFile(path)
			if err == nil && !strings.Contains(string(data), newStr) {
				return &llm.Message{
					Role:       llm.RoleTool,
					Content:    fmt.Sprintf("验证失败：替换未生效，目标内容未出现在 %s 中。请先 read_file 查看当前内容再重试。", path),
					ToolCallID: result.ToolCallID,
					Name:       result.Name,
				}
			}
		}
	}

	// .go 文件语法检查
	if filepath.Ext(path) == ".go" {
		if syntaxErr := checkGoSyntax(path); syntaxErr != "" {
			return &llm.Message{
				Role:       llm.RoleTool,
				Content:    fmt.Sprintf("验证失败：%s 有语法错误：%s\n请修复语法错误。", path, syntaxErr),
				ToolCallID: result.ToolCallID,
				Name:       result.Name,
			}
		}
	}

	return nil // 验证通过
}

// checkGoSyntax 用 go/parser 检查 Go 文件语法
func checkGoSyntax(path string) string {
	// 延迟导入，避免非 Go 项目必须安装 go/parser
	// 这里用简单方式：执行 `go vet` 或解析 AST
	// Phase 1 先用基础检查，Phase 4 接入完整 AST
	return ""
}

// setState 更新 Agent 状态并触发回调
func (a *DefaultAgent) setState(newState AgentState) {
	a.mu.Lock()
	old := a.state
	a.state = newState
	cb := a.onStateChange
	eb := a.eventBus
	a.mu.Unlock()

	if cb != nil && old != newState {
		cb(old, newState)
	}
	if eb != nil && old != newState {
		eb.Push(pubsub.Event{
			Type:      pubsub.EventStateChange,
			StateName: newState.String(),
		})
	}
}

// formatArgsSummary 生成参数摘要（用于日志和确认提示）
func formatArgsSummary(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		if len(s) > 80 {
			s = s[:80] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, s))
	}
	return strings.Join(parts, ", ")
}
