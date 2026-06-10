package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lix-lang/codebuddy/internal/pubsub"
)

// ================================================================
// 样式
// ================================================================

var (
	userPromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)

	toolLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	toolResultLineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("59"))

	errLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	sysLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("236"))

	welcomeBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2)

	welcomeTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86")).
				Bold(true)

	welcomeLogoStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("62"))

	welcomeSubStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("246"))

	welcomeKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("180"))

	welcomeDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("246"))
)

// ChatModel 聊天区子模型
type ChatModel struct {
	vp viewport.Model

	mu        *sync.Mutex
	lines     []string // 已完成的行
	streamBuf string   // 流式接收中的内容（纯文本）
	lastRole  string

	lastRender time.Time
	dirty      bool
	modelName  string
	width      int
	height     int
}

// NewChatModel 创建聊天区模型
func NewChatModel(width, height int, modelName string) *ChatModel {
	vp := viewport.New(width, height)
	vp.Style = lipgloss.NewStyle().PaddingLeft(1)

	c := &ChatModel{
		vp:        vp,
		width:     width,
		height:    height,
		mu:        &sync.Mutex{},
		modelName: modelName,
	}
	// 欢迎屏作为 viewport 初始内容，用户发消息后自然被替换
	c.vp.SetContent(c.buildWelcome())
	return c
}

// SetSize 调整尺寸
func (c *ChatModel) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.vp.Width = width
	c.vp.Height = height
}

// HandleEvent 处理事件总线的事件
func (c *ChatModel) HandleEvent(e pubsub.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch e.Type {
	case pubsub.EventContent:
		c.streamBuf += e.Content
		c.dirty = true

	case pubsub.EventToolCall:
		c.flushStream()
		args := truncate(e.ToolArgs, 50)
		line := toolLineStyle.Render(fmt.Sprintf("  ⎿ %s(%s)", e.ToolName, args))
		c.lines = append(c.lines, line)
		c.lastRole = "tool"
		c.dirty = true

	case pubsub.EventToolResult:
		result := truncate(e.ToolResult, 80)
		line := toolResultLineStyle.Render(fmt.Sprintf("    → %s", result))
		c.lines = append(c.lines, line)
		c.dirty = true

	case pubsub.EventError:
		c.flushStream()
		line := errLineStyle.Render(fmt.Sprintf("  ⚠ %s", e.Err.Error()))
		c.lines = append(c.lines, line)
		c.dirty = true
	}
}

// AddUserMessage 添加用户消息
func (c *ChatModel) AddUserMessage(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.flushStream()

	if len(c.lines) > 0 {
		sep := separatorStyle.Render("  " + strings.Repeat("·", min(c.width-4, 40)))
		c.lines = append(c.lines, sep)
	}

	line := userPromptStyle.Render("❯ ") + content
	c.lines = append(c.lines, line)
	c.lastRole = "user"
	c.dirty = true
}

// AddSystemMessage 添加系统消息
func (c *ChatModel) AddSystemMessage(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.flushStream()
	line := sysLineStyle.Render("  " + content)
	c.lines = append(c.lines, line)
	c.dirty = true
}

// AddErrorMessage 添加错误消息
func (c *ChatModel) AddErrorMessage(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.flushStream()
	line := errLineStyle.Render(fmt.Sprintf("  ⚠ %s", content))
	c.lines = append(c.lines, line)
	c.dirty = true
}

// Flush 强制刷新渲染
func (c *ChatModel) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushStream()
	c.forceRender()
}

// Clear 清空聊天区
func (c *ChatModel) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = nil
	c.streamBuf = ""
	c.lastRole = ""
	c.dirty = false
	c.vp.SetContent(c.buildWelcome())
}

// flushStream 把流式缓冲加入已完成行
func (c *ChatModel) flushStream() {
	if c.streamBuf == "" {
		return
	}
	c.lines = append(c.lines, c.streamBuf)
	c.streamBuf = ""
	c.lastRole = "assistant"
}

func (c *ChatModel) tryRender() {
	if !c.dirty {
		return
	}
	now := time.Now()
	if now.Sub(c.lastRender) < 100*time.Millisecond {
		return
	}
	c.lastRender = now
	c.doRender()
	c.dirty = false
}

func (c *ChatModel) forceRender() {
	c.doRender()
	c.dirty = false
	c.lastRender = time.Now()
}

// doRender 组装最终内容设给 viewport
func (c *ChatModel) doRender() {
	var sb strings.Builder

	for i, line := range c.lines {
		sb.WriteString(line)
		if i < len(c.lines)-1 || c.streamBuf != "" {
			sb.WriteString("\n")
		}
	}

	// 流式内容直接追加纯文本
	if c.streamBuf != "" {
		sb.WriteString(c.streamBuf)
		sb.WriteString("▌")
	}

	c.vp.SetContent(sb.String())
	c.vp.GotoBottom()
}

// Update 处理 bubbletea 消息
func (c *ChatModel) Update(msg tea.Msg) (*ChatModel, tea.Cmd) {
	var cmd tea.Cmd
	c.vp, cmd = c.vp.Update(msg)
	return c, cmd
}

// View 渲染聊天区 — 始终返回 viewport，不再条件判断欢迎屏
func (c *ChatModel) View() string {
	c.mu.Lock()
	if c.dirty {
		c.tryRender()
	}
	c.mu.Unlock()
	return c.vp.View()
}

// ================================================================
// 欢迎屏
// ================================================================

func (c *ChatModel) buildWelcome() string {
	boxW := min(c.width-4, 72)
	if boxW < 30 {
		boxW = 30
	}

	// Logo
	logo := welcomeLogoStyle.Render(
		"  ╭─────────╮\n" +
			"  │  ◈ CB   │\n" +
			"  ╰─────────╯")

	// 标题行
	title := welcomeTitleStyle.Render("Codebuddy")
	sub := welcomeSubStyle.Render(c.modelName + " · AI pair programmer")

	// 快捷键
	keys := []string{
		welcomeKeyStyle.Render("  Enter     ") + "Send message",
		welcomeKeyStyle.Render("  Shift+↵   ") + "New line",
		welcomeKeyStyle.Render("  Ctrl+C    ") + "Interrupt",
		welcomeKeyStyle.Render("  /help     ") + "All commands",
		welcomeKeyStyle.Render("  /clear    ") + "Clear chat",
		welcomeKeyStyle.Render("  /cost     ") + "Token usage",
	}

	var sb strings.Builder
	sb.WriteString(logo)
	sb.WriteString("\n\n")
	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString(sub)
	sb.WriteString("\n\n")
	for _, k := range keys {
		sb.WriteString(welcomeDescStyle.Render(k))
		sb.WriteString("\n")
	}

	box := welcomeBoxStyle.Width(boxW).Render(sb.String())
	return lipgloss.Place(c.width, c.height, lipgloss.Center, lipgloss.Center, box)
}

// ================================================================
// 工具函数
// ================================================================

func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "…"
}
