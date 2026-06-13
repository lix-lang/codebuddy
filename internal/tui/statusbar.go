package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ================================================================
// StatusBarModel 状态栏模型
// ================================================================

// spinner 动画帧
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// 状态配置
var stateConfig = map[string]struct {
	label string
	color string
	spin  bool
}{
	"idle":       {"", "243", false},
	"thinking":   {"thinking", "214", true},
	"executing":  {"working", "39", true},
	"confirming": {"confirm?", "214", false},
}

// StatusBarModel 状态栏子模型
type StatusBarModel struct {
	modelName  string
	state      string
	totalToken int
	totalCost  float64
	toolCount  int
	maxSteps   int
	maxContext int
	width      int
	spinIdx    int // spinner 动画帧索引
}

// StatusBarConfig 状态栏配置
type StatusBarConfig struct {
	ModelName  string
	MaxSteps   int
	MaxContext int
}

// NewStatusBarModel 创建状态栏模型
func NewStatusBarModel(cfg StatusBarConfig, width int) StatusBarModel {
	return StatusBarModel{
		modelName:  cfg.ModelName,
		state:      "idle",
		maxSteps:   cfg.MaxSteps,
		maxContext: cfg.MaxContext,
		width:      width,
	}
}

// SetSize 调整尺寸
func (s *StatusBarModel) SetSize(width int) {
	s.width = width
}

// SetState 设置 Agent 状态
func (s *StatusBarModel) SetState(state string) {
	s.state = state
}

// SetUsage 更新使用量
func (s *StatusBarModel) SetUsage(tokens int, cost float64) {
	s.totalToken = tokens
	s.totalCost = cost
}

// SetToolCount 更新工具调用次数
func (s *StatusBarModel) SetToolCount(count int) {
	s.toolCount = count
}

// Tick 推进 spinner 动画，返回 true 表示需要重绘
func (s *StatusBarModel) Tick() bool {
	sc, ok := stateConfig[s.state]
	if !ok || !sc.spin {
		return false
	}
	s.spinIdx = (s.spinIdx + 1) % len(spinnerFrames)
	return true
}

// View 渲染状态栏
//
// 风格：一条细分隔线，状态信息嵌入其中，左右两端对齐
// 类似 Claude Code 的底部状态栏
func (s StatusBarModel) View() string {
	w := s.width
	if w < 20 {
		w = 20
	}

	// 构建中间内容
	middle := s.renderMiddle()

	// 用分隔线填充两侧
	sepColor := lipgloss.Color("236") // 暗灰分隔线
	sepStyle := lipgloss.NewStyle().Foreground(sepColor)

	midLen := lipgloss.Width(middle)
	leftLen := (w - midLen) / 2
	rightLen := w - midLen - leftLen
	if rightLen < 0 {
		rightLen = 0
	}

	left := sepStyle.Render(strings.Repeat("─", leftLen))
	right := sepStyle.Render(strings.Repeat("─", rightLen))

	return left + middle + right
}

// renderMiddle 渲染状态栏：进度条 + token + 模型名
func (s StatusBarModel) renderMiddle() string {
	var parts []string

	// 上下文进度条 + token
	if s.maxContext > 0 && s.totalToken > 0 {
		pct := float64(s.totalToken) / float64(s.maxContext)
		bar := renderContextBar(pct, 10)
		parts = append(parts, bar)
		tokenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		parts = append(parts, tokenStyle.Render(fmtToken(s.totalToken)))
	}

	// 步数（有工具调用时才显示）
	if s.toolCount > 0 {
		stepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		parts = append(parts, stepStyle.Render(fmt.Sprintf("step %d/%d", s.toolCount, s.maxSteps)))
	}

	// 费用
	if s.totalCost >= 0.01 {
		costStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		parts = append(parts, costStyle.Render(fmt.Sprintf("$%.2f", s.totalCost)))
	}

	// 模型名始终显示
	modelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	parts = append(parts, modelStyle.Render(s.modelName))

	joinStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	return joinStyle.Render(" " + strings.Join(parts, " · ") + " ")
}

// fmtToken 格式化 token 数
func fmtToken(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// renderContextBar 渲染上下文使用进度条
// pct 是使用比例 (0.0~1.0)，width 是进度条字符宽度
// 颜色: 绿(<60%) → 黄(60-85%) → 红(>85%)
func renderContextBar(pct float64, width int) string {
	if pct > 1.0 {
		pct = 1.0
	}
	filled := int(pct * float64(width))

	// 选择颜色
	var color lipgloss.Color
	switch {
	case pct < 0.6:
		color = lipgloss.Color("82") // 绿
	case pct < 0.85:
		color = lipgloss.Color("214") // 黄
	default:
		color = lipgloss.Color("196") // 红
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	pctStr := fmt.Sprintf("%d%%", int(pct*100))

	style := lipgloss.NewStyle().Foreground(color)
	return style.Render(bar + " " + pctStr)
}
