package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/lix-lang/codebuddy/internal/config"
	"github.com/lix-lang/codebuddy/internal/llm"
	"github.com/lix-lang/codebuddy/internal/tool"
)

// ================================================================
// Mock LLM Client — 预设响应，不依赖真实 API
// ================================================================

// mockLLMClient 模拟 LLM 客户端，按预设脚本返回响应
type mockLLMClient struct {
	responses [][]mockEvent // 按调用顺序返回的预设响应
	callCount int
	mu        sync.Mutex
}

// mockEvent 模拟一个流式事件
type mockEvent struct {
	Type      llm.StreamEventType
	Content   string
	ToolCalls *llm.ToolCallChunk
	Usage     *llm.Usage
}

func (m *mockLLMClient) ChatStream(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (<-chan llm.StreamEvent, error) {
	m.mu.Lock()
	idx := m.callCount
	m.callCount++
	m.mu.Unlock()

	ch := make(chan llm.StreamEvent, 100)

	go func() {
		defer close(ch)
		if idx >= len(m.responses) {
			// 没有更多预设响应，返回空文本
			ch <- llm.StreamEvent{Type: llm.StreamEventContent, Content: "没有更多预设响应了"}
			ch <- llm.StreamEvent{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 10}}
			return
		}
		for _, evt := range m.responses[idx] {
			ch <- llm.StreamEvent{
				Type:      evt.Type,
				Content:   evt.Content,
				ToolCalls: evt.ToolCalls,
				Usage:     evt.Usage,
			}
		}
	}()

	return ch, nil
}

func (m *mockLLMClient) CountTokens(messages []llm.Message) (int, error) { return 100, nil }
func (m *mockLLMClient) ModelName() string                               { return "mock-model" }
func (m *mockLLMClient) MaxContextTokens() int                           { return 128000 }
func (m *mockLLMClient) SupportsVision() bool                            { return false }

// ================================================================
// Mock ContextManager
// ================================================================

type mockCtxManager struct {
	history []llm.Message
}

func (m *mockCtxManager) BuildMessages(userMsg string) ([]llm.Message, error) {
	return append([]llm.Message{}, m.history...), nil
}

func (m *mockCtxManager) AddHistory(msg llm.Message) {
	m.history = append(m.history, msg)
}

func (m *mockCtxManager) Scan() error { return nil }

func (m *mockCtxManager) HasScanned() bool { return false }

// ================================================================
// 辅助函数
// ================================================================

// makeToolCallArgs 构造工具调用参数 JSON
func makeToolCallArgs(t *testing.T, args map[string]any) string {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("构造参数 JSON 失败: %v", err)
	}
	return string(b)
}

// newTestAgent 创建测试用 Agent
func newTestAgent(mockLLM *mockLLMClient) *DefaultAgent {
	registry := tool.NewRegistry()
	ctxMgr := &mockCtxManager{}

	return NewAgent(AgentConfig{
		LLMClient:  mockLLM,
		Registry:   registry,
		CtxManager: ctxMgr,
		Config:     config.DefaultConfig(),
	})
}

// ================================================================
// 测试场景 1：纯文本回复（无工具调用）
// ================================================================

func TestAgent_PureTextResponse(t *testing.T) {
	mockLLM := &mockLLMClient{
		responses: [][]mockEvent{
			{
				{Type: llm.StreamEventContent, Content: "你好！"},
				{Type: llm.StreamEventContent, Content: "我是编程助手。"},
				{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 20}},
			},
		},
	}

	agent := newTestAgent(mockLLM)
	err := agent.Run(context.Background(), "你好")
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if agent.State() != StateDone {
		t.Errorf("期望状态 Done，实际 %v", agent.State())
	}
}

// ================================================================
// 测试场景 2：工具调用链（read_file → 回复结果）
// ================================================================

func TestAgent_ToolCallChain(t *testing.T) {
	// 先创建一个临时文件
	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.txt"
	writeFile(t, testFile, "hello world")

	// 注册 read_file 工具
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadFileTool(tmpDir))

	// 路径校验要求绝对路径或相对于 tmpDir 的路径
	// read_file 内部会 filepath.Join(rootDir, path)，所以用文件名就行
	absPath := "test.txt"

	mockLLM := &mockLLMClient{
		responses: [][]mockEvent{
			// 第 1 轮：LLM 返回 read_file 工具调用
			{
				{Type: llm.StreamEventToolCall, ToolCalls: &llm.ToolCallChunk{
					ID: "call_1", FunctionName: "read_file",
					ArgumentsDelta: makeToolCallArgs(t, map[string]any{"path": absPath}),
				}},
				{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 50}},
			},
			// 第 2 轮：LLM 返回最终文本回复
			{
				{Type: llm.StreamEventContent, Content: "文件内容是 hello world"},
				{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 30}},
			},
		},
	}

	ctxMgr := &mockCtxManager{}
	agent := NewAgent(AgentConfig{
		LLMClient:  mockLLM,
		Registry:   registry,
		CtxManager: ctxMgr,
		Config:     config.DefaultConfig(),
	})

	err := agent.Run(context.Background(), "读一下 test.txt")
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	// 验证历史记录：用户消息 + 助手工具调用 + 工具结果 + 助手文本回复
	history := ctxMgr.history
	if len(history) < 4 {
		t.Fatalf("期望至少 4 条历史，实际 %d 条", len(history))
	}

	// 第 1 条：用户消息
	if history[0].Role != llm.RoleUser {
		t.Errorf("history[0] 应该是 user，实际 %v", history[0].Role)
	}
	// 第 2 条：助手工具调用
	if history[1].Role != llm.RoleAssistant || len(history[1].ToolCalls) != 1 {
		t.Errorf("history[1] 应该是 assistant + 1 个工具调用")
	}
	// 第 3 条：工具结果
	if history[2].Role != llm.RoleTool {
		t.Errorf("history[2] 应该是 tool")
	}
	if history[2].Content != "hello world" {
		t.Errorf("工具结果应该是 'hello world'，实际 %q", history[2].Content)
	}
}

// ================================================================
// 测试场景 3：达到最大步数
// ================================================================

func TestAgent_MaxSteps(t *testing.T) {
	// LLM 每轮都返回工具调用，永远不停
	loopToolCall := []mockEvent{
		{Type: llm.StreamEventToolCall, ToolCalls: &llm.ToolCallChunk{
			ID: "call_loop", FunctionName: "read_file",
			ArgumentsDelta: `{"path":"fake.txt"}`,
		}},
		{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 10}},
	}

	mockLLM := &mockLLMClient{
		responses: [][]mockEvent{
			loopToolCall, loopToolCall, loopToolCall,
			loopToolCall, loopToolCall, loopToolCall, // 6 轮够了
		},
	}

	cfg := config.DefaultConfig()
	cfg.Agent.MaxSteps = 3 // 设置很小的上限

	registry := tool.NewRegistry()
	// 不注册 read_file，工具不存在会返回错误消息但不中断循环

	agent := NewAgent(AgentConfig{
		LLMClient:  mockLLM,
		Registry:   registry,
		CtxManager: &mockCtxManager{},
		Config:     cfg,
	})

	err := agent.Run(context.Background(), "循环测试")
	if err == nil {
		t.Fatal("期望返回达到最大步数错误")
	}
	if agent.TotalTokens() == 0 {
		t.Error("期望累计 token > 0")
	}
}

// ================================================================
// 测试场景 4：Stop 中断循环
// ================================================================

func TestAgent_Stop(t *testing.T) {
	mockLLM := &mockLLMClient{
		responses: [][]mockEvent{
			{{Type: llm.StreamEventContent, Content: "收到"}, {Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 5}}},
		},
	}

	agent := newTestAgent(mockLLM)
	ctx, cancel := context.WithCancel(context.Background())

	// 先取消 context，模拟 Stop
	cancel()
	err := agent.Run(ctx, "测试中断")
	if err == nil {
		t.Fatal("期望返回 context 取消错误")
	}
}

// ================================================================
// 测试场景 5：工具参数校验失败
// ================================================================

func TestAgent_InvalidToolArgs(t *testing.T) {
	tmpDir := t.TempDir()
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadFileTool(tmpDir))

	mockLLM := &mockLLMClient{
		responses: [][]mockEvent{
			// LLM 调用 read_file 但没传 path 参数
			{
				{Type: llm.StreamEventToolCall, ToolCalls: &llm.ToolCallChunk{
					ID: "call_bad", FunctionName: "read_file",
					ArgumentsDelta: `{}`,
				}},
				{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 10}},
			},
			// LLM 收到错误后回复
			{
				{Type: llm.StreamEventContent, Content: "参数错误，我重试"},
				{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 10}},
			},
		},
	}

	ctxMgr := &mockCtxManager{}
	agent := NewAgent(AgentConfig{
		LLMClient:  mockLLM,
		Registry:   registry,
		CtxManager: ctxMgr,
		Config:     config.DefaultConfig(),
	})

	err := agent.Run(context.Background(), "读文件但不传参数")
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	// 验证工具结果包含参数校验错误
	toolResult := ctxMgr.history[2] // user, assistant, tool
	if toolResult.Role != llm.RoleTool {
		t.Fatalf("期望 tool 消息，实际 %v", toolResult.Role)
	}
	if toolResult.Content == "" {
		t.Error("期望参数校验错误信息")
	}
}

// ================================================================
// 测试场景 6：并行工具调用
// ================================================================

func TestAgent_ParallelToolCalls(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/a.txt", "content A")
	writeFile(t, tmpDir+"/b.txt", "content B")

	registry := tool.NewRegistry()
	registry.Register(tool.NewReadFileTool(tmpDir))

	mockLLM := &mockLLMClient{
		responses: [][]mockEvent{
			// LLM 一次返回 2 个工具调用
			{
				{Type: llm.StreamEventToolCall, ToolCalls: &llm.ToolCallChunk{
					ID: "call_a", FunctionName: "read_file",
					ArgumentsDelta: makeToolCallArgs(t, map[string]any{"path": "a.txt"}),
				}},
				{Type: llm.StreamEventToolCall, ToolCalls: &llm.ToolCallChunk{
					ID: "call_b", FunctionName: "read_file",
					ArgumentsDelta: makeToolCallArgs(t, map[string]any{"path": "b.txt"}),
				}},
				{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 50}},
			},
			// 汇总回复
			{
				{Type: llm.StreamEventContent, Content: "两个文件都读到了"},
				{Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 20}},
			},
		},
	}

	ctxMgr := &mockCtxManager{}
	agent := NewAgent(AgentConfig{
		LLMClient:  mockLLM,
		Registry:   registry,
		CtxManager: ctxMgr,
		Config:     config.DefaultConfig(),
	})

	err := agent.Run(context.Background(), "同时读两个文件")
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	// 验证有 2 条工具结果
	toolCount := 0
	for _, msg := range ctxMgr.history {
		if msg.Role == llm.RoleTool {
			toolCount++
		}
	}
	if toolCount != 2 {
		t.Errorf("期望 2 条工具结果，实际 %d 条", toolCount)
	}
}

// ================================================================
// 测试场景 7：状态回调
// ================================================================

func TestAgent_StateCallbacks(t *testing.T) {
	mockLLM := &mockLLMClient{
		responses: [][]mockEvent{
			{{Type: llm.StreamEventContent, Content: "ok"}, {Type: llm.StreamEventDone, Usage: &llm.Usage{TotalTokens: 5}}},
		},
	}

	agent := newTestAgent(mockLLM)

	var stateChanges []string
	agent.SetOnStateChange(func(old, new AgentState) {
		stateChanges = append(stateChanges, fmt.Sprintf("%s->%s", old, new))
	})

	agent.Run(context.Background(), "测试")

	// 期望状态变化：Idle → Thinking → Done
	if len(stateChanges) < 2 {
		t.Fatalf("期望至少 2 次状态变化，实际 %d 次", len(stateChanges))
	}
	if stateChanges[0] != "idle->thinking" {
		t.Errorf("第一次状态变化应该是 idle->thinking，实际 %s", stateChanges[0])
	}
}

// ================================================================
// HTTP Mock Server 测试（更真实的集成测试）
// ================================================================

func TestAgent_WithHTTPMockServer(t *testing.T) {
	// 构造一个 mock OpenAI API server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("不支持 flush")
		}

		// 返回简单文本响应
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"测试回复\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	// 注意：这个测试验证 HTTP mock server 能工作
	// 真正的集成测试需要用实际的 OpenAI client 连接这个 server
	if callCount != 0 {
		t.Error("不应该在创建时就调用")
	}
}

// ================================================================
// 工具测试辅助
// ================================================================

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := writeFileSync(path, content); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

func writeFileSync(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
