package tui

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lix-lang/codebuddy/internal/agent"
	"github.com/lix-lang/codebuddy/internal/config"
	"github.com/lix-lang/codebuddy/internal/pubsub"
)

// ================================================================
// 消息类型
// ================================================================

type eventMsg struct{ event pubsub.Event }
type spinnerTickMsg struct{}
type agentDoneMsg struct{ err error }
type confirmRequestMsg struct{ req pubsub.ConfirmRequest }

// confirmBridge 将确认请求从 agent goroutine 传到 bubbletea 主循环
var confirmBridge = make(chan pubsub.ConfirmRequest, 8)

// stripANSI 清除终端转义序列
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|;rgb:[0-9a-f/]+|\[[0-9]+;[0-9]+[a-zA-Z]`)

// cprRe 匹配终端光标位置报告
var cprRe = regexp.MustCompile(`\d+;\d+R`)

// isGarbageKey 过滤终端转义垃圾（CPR、DA 响应、OSC 响应等）
func isGarbageKey(msg tea.KeyMsg) bool {
	s := msg.String()
	// 光标位置报告 (CPR): "52;30R"
	if cprRe.MatchString(s) {
		return true
	}
	// 设备属性响应 (DA): "?64;1;2c" 或类似
	if strings.HasPrefix(s, "?") && len(s) > 2 {
		return true
	}
	// Kitty/XTVersion 等响应
	if strings.Contains(s, ";rgb:") ||
		strings.Contains(s, "\x1b]") ||
		strings.Contains(s, "\x1bP") {
		return true
	}
	// 以 ';' 开头的非可打印序列
	if len(s) > 2 && s[0] == ';' && strings.Contains(s, "rgb") {
		return true
	}
	return false
}

// ================================================================
// 主 Model
// ================================================================

// Model bubbletea 主模型
type Model struct {
	chat   *ChatModel
	input  InputModel
	status StatusBarModel

	agent *agent.DefaultAgent
	cfg   config.Config
	eb    *pubsub.EventBus

	confirming *pubsub.ConfirmRequest
	state      string // "idle", "thinking", "executing", "confirming"
	width      int
	height     int
	toolStep   int
}

// NewModel 创建 TUI 模型
func NewModel(a *agent.DefaultAgent, cfg config.Config, eb *pubsub.EventBus) Model {
	maxSteps := cfg.Agent.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 15
	}

	return Model{
		chat:   NewChatModel(80, 18, cfg.LLM.Model),
		input:  NewInputModel(80),
		status: NewStatusBarModel(StatusBarConfig{ModelName: cfg.LLM.Model, MaxSteps: maxSteps, MaxContext: 128000}, 80),
		agent:  a,
		cfg:    cfg,
		eb:     eb,
		state:  "idle",
		width:  80,
		height: 24,
	}
}

// Init 初始化
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.input.Focus(),
		m.waitForEvent(),
		m.waitForConfirm(),
		spinnerTick(),
	)
}

func spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(_ time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// Update 处理所有消息
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chat.SetSize(msg.Width, m.height-6)
		m.input.SetSize(msg.Width)
		m.status.SetSize(msg.Width)
		if !m.input.disabled {
			cmds = append(cmds, m.input.Focus())
		}
		log.Printf("[WINSIZE] %dx%d", msg.Width, msg.Height)

	case tea.KeyMsg:
		if isGarbageKey(msg) {
			return m, nil
		}
		return m.handleKeyMsg(msg)

	case eventMsg:
		m.handleEventMsg(msg.event)
		cmds = append(cmds, m.waitForEvent())

	case agentDoneMsg:
		m.state = "idle"
		m.toolStep = 0
		m.status.SetState("idle")
		m.status.SetToolCount(0)
		// 更新 usage，如果 SSE 没返回精确数据则用估算
		tokens := m.agent.TotalTokens()
		if tokens == 0 {
			tokens = m.estimateTokens()
		}
		m.status.SetUsage(tokens, m.agent.TotalCost())
		m.input.disabled = false
		cmds = append(cmds, m.input.Focus())
		m.chat.Flush()
		if msg.err != nil {
			m.chat.AddErrorMessage(fmt.Sprintf("Agent error: %v", msg.err))
		}

	case spinnerTickMsg:
		m.status.Tick()
		// 动态计算 token（优先用精确值，否则从聊天内容估算）
		if tokens := m.agent.TotalTokens(); tokens > 0 {
			m.status.SetUsage(tokens, m.agent.TotalCost())
		} else if m.state != "idle" {
			m.status.SetUsage(m.estimateTokens(), 0)
		}
		cmds = append(cmds, spinnerTick())

	case confirmRequestMsg:
		m.confirming = &msg.req
		m.state = "confirming"
		m.status.SetState("confirming")
		cmds = append(cmds, m.waitForConfirm())
	}

	// 子模型更新（非 KeyMsg 才走到这里）
	var cmd tea.Cmd
	newChat, cmd := m.chat.Update(msg)
	m.chat = newChat
	cmds = append(cmds, cmd)

	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// handleKeyMsg 处理键盘消息
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirming != nil {
		return m.handleConfirmKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		if m.state != "idle" {
			m.agent.Stop()
			m.chat.AddSystemMessage("已中断")
			m.state = "idle"
			m.status.SetState("idle")
			m.input.SetDisabled(false)
			return m, m.input.Focus()
		}
		return m, tea.Quit

	case tea.KeyEnter:
		raw := ansiRe.ReplaceAllString(m.input.Value(), "")
		input := strings.TrimSpace(raw)
		if input == "" {
			m.input.Reset()
			return m, nil
		}
		m.input.Reset()
		return m.handleInput(input)
	}

	// Shift+Enter 多行模式（CSI-u 编码）
	if msg.String() == "\x1b[13;2u" {
		if !m.input.IsMultiline() && m.state == "idle" {
			m.input.SetMultiline(true)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.confirming.Response <- true
		m.confirming = nil
		m.state = "executing"
		m.status.SetState("executing")
		m.chat.AddSystemMessage("已确认")
	case "n", "N":
		m.confirming.Response <- false
		m.confirming = nil
		m.state = "idle"
		m.status.SetState("idle")
		m.chat.AddSystemMessage("已取消")
	}
	return m, nil
}

func (m Model) handleInput(input string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(input, "/") {
		// 先尝试匹配斜杠命令，不匹配则当普通消息
		if cmd, ok := agent.ParseCommand(input); ok {
			return m.handleSlashCommand(cmd)
		}
	}
	m.chat.AddUserMessage(input)
	m.state = "thinking"
	m.status.SetState("thinking")
	m.input.SetDisabled(true)
	m.toolStep = 0
	return m, m.runAgent(input)
}

func (m Model) handleEventMsg(e pubsub.Event) {
	switch e.Type {
	case pubsub.EventContent:
		m.chat.HandleEvent(e)
	case pubsub.EventToolCall:
		m.toolStep++
		m.status.SetToolCount(m.toolStep)
		m.chat.HandleEvent(e)
	case pubsub.EventToolResult:
		m.chat.HandleEvent(e)
	case pubsub.EventStateChange:
		m.state = e.StateName
		m.status.SetState(e.StateName)
	case pubsub.EventError:
		m.chat.HandleEvent(e)
	case pubsub.EventUsageUpdate:
		m.status.SetUsage(e.TotalTokens, e.TotalCost)
	}
}

// View 渲染界面
func (m Model) View() string {
	// 状态指示行（thinking/working 等，显示在输入框下方）
	statusLine := ""
	if sc, ok := stateConfig[m.state]; ok && sc.label != "" {
		spin := "●"
		if sc.spin {
			spin = spinnerFrames[m.status.spinIdx]
		}
		statusLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color(sc.color)).
			Render(fmt.Sprintf("  %s %s", spin, sc.label))
	}

	// 确认提示
	confirmView := ""
	if m.confirming != nil {
		confirmView = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).Bold(true).
			Render(fmt.Sprintf("  ⚠ allow %s(%s)? [y/n] ",
				m.confirming.ToolName, truncate(m.confirming.Args, 50)))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.chat.View(),
		m.status.View(),
		confirmView,
		m.input.View(),
		statusLine,
	)
}

// ================================================================
// Agent 异步运行
// ================================================================

func (m Model) runAgent(userMsg string) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[AGENT] panic: %v", r)
				msg = agentDoneMsg{err: fmt.Errorf("agent panic: %v", r)}
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := m.agent.Run(ctx, userMsg)
		return agentDoneMsg{err: err}
	}
}

func (m Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		e, ok := <-m.eb.Subscribe()
		if !ok {
			return nil
		}
		return eventMsg{event: e}
	}
}

// estimateTokens 粗略估算本次会话的 token 数（SSE 未返回 usage 时使用）
func (m Model) estimateTokens() int {
	total := 0
	for _, line := range m.chat.lines {
		total += len(line) / 3
	}
	total += len(m.chat.streamBuf) / 3
	if total == 0 {
		total = 1 // 至少显示进度条
	}
	return total
}

func (m Model) waitForConfirm() tea.Cmd {
	return func() tea.Msg {
		req, ok := <-confirmBridge
		if !ok {
			return nil
		}
		return confirmRequestMsg{req: req}
	}
}

// ================================================================
// 斜杠命令
// ================================================================

func (m Model) handleSlashCommand(cmd agent.Command) (tea.Model, tea.Cmd) {
	switch cmd.Type {
	case agent.CmdHelp:
		m.chat.AddSystemMessage(agent.FormatHelp())
	case agent.CmdClear:
		m.chat.Clear()
		m.chat.AddSystemMessage("对话已清空")
	case agent.CmdContext:
		m.chat.AddSystemMessage(fmt.Sprintf("模型: %s\n状态: %s", m.cfg.LLM.Model, m.state))
	case agent.CmdCost:
		m.chat.AddSystemMessage(fmt.Sprintf("Token: %d\n费用: %.2f", m.agent.TotalTokens(), m.agent.TotalCost()))
	case agent.CmdCompact:
		m.chat.AddSystemMessage("手动压缩上下文（功能开发中）")
	case agent.CmdModel:
		if cmd.Args == "" {
			m.chat.AddSystemMessage(fmt.Sprintf("当前模型: %s", m.cfg.LLM.Model))
		} else {
			m.chat.AddSystemMessage(fmt.Sprintf("切换模型到: %s（重启生效）", cmd.Args))
		}
	case agent.CmdScan:
		if m.agent.HasScanned() {
			m.chat.AddSystemMessage("项目已扫描，重新扫描中...")
		} else {
			m.chat.AddSystemMessage("正在扫描项目...")
		}
		if err := m.agent.ScanProject(); err != nil {
			m.chat.AddErrorMessage(fmt.Sprintf("扫描失败: %v", err))
		} else {
			m.chat.AddSystemMessage("项目扫描完成")
		}
	default:
		m.chat.AddSystemMessage(fmt.Sprintf("命令 %s 功能开发中", string(cmd.Type)))
	}
	return m, nil
}

// ================================================================
// 入口
// ================================================================

// Run 启动 TUI
func Run(a *agent.DefaultAgent, cfg config.Config) error {
	log.Printf("[RUN] model=%s", cfg.LLM.Model)

	eb := pubsub.NewEventBus()
	a.SetEventBus(eb)
	a.SetOnConfirmRequest(func(req pubsub.ConfirmRequest) {
		confirmBridge <- req
	})

	model := NewModel(a, cfg, eb)
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stdout),
	)

	_, runErr := p.Run()
	return runErr
}
