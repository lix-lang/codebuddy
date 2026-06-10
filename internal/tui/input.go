package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ================================================================
// InputModel 输入框模型
// ================================================================

var (
	// 用 PromptStyle 而非 Render()，避免 ANSI 转义码干扰 textinput 光标
	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86")).
				Bold(true)

	inputHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))

	inputDisabledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("238"))

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	inputBoxDisabledStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("236")).
				Padding(0, 1)
)

// InputModel 输入区子模型
type InputModel struct {
	input     textinput.Model
	multiline bool
	multiBuf  strings.Builder
	disabled  bool
	width     int
}

// NewInputModel 创建输入框模型
func NewInputModel(width int) InputModel {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.PromptStyle = inputPromptStyle
	ti.Placeholder = "ask me anything…"
	ti.CharLimit = 10000
	ti.Width = width - 4
	ti.Focus()

	return InputModel{
		input: ti,
		width: width,
	}
}

// SetSize 调整尺寸
func (im *InputModel) SetSize(width int) {
	im.width = width
	im.input.Width = width - 4
}

// SetDisabled 设置禁用状态
func (im *InputModel) SetDisabled(disabled bool) {
	im.disabled = disabled
	if disabled {
		im.input.Blur()
	} else {
		im.input.Focus()
	}
}

// Value 获取当前完整输入值（含多行缓冲）
func (im *InputModel) Value() string {
	if im.multiline {
		return im.multiBuf.String() + "\n" + im.input.Value()
	}
	return im.input.Value()
}

// Reset 重置输入框
func (im *InputModel) Reset() {
	im.input.Reset()
	im.multiline = false
	im.multiBuf.Reset()
	im.input.Prompt = "❯ "
	im.input.PromptStyle = inputPromptStyle
}

// Focus 聚焦输入框
func (im *InputModel) Focus() tea.Cmd {
	return im.input.Focus()
}

// Blur 取消聚焦
func (im *InputModel) Blur() {
	im.input.Blur()
}

// IsMultiline 是否处于多行模式
func (im *InputModel) IsMultiline() bool {
	return im.multiline
}

// SetMultiline 设置多行模式
func (im *InputModel) SetMultiline(m bool) {
	im.multiline = m
	if m {
		im.multiBuf.WriteString(im.input.Value())
		im.multiBuf.WriteString("\n")
		im.input.Reset()
		im.input.Prompt = "… "
	} else {
		im.input.Prompt = "❯ "
		im.input.PromptStyle = inputPromptStyle
	}
}

// Update 处理 bubbletea 消息
func (im InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	var cmd tea.Cmd
	im.input, cmd = im.input.Update(msg)
	return im, cmd
}

// View 渲染输入区
func (im InputModel) View() string {
	if im.disabled {
		return inputBoxDisabledStyle.Width(im.width - 4).Render("  waiting…")
	}

	var inner strings.Builder

	if im.multiline {
		lines := strings.Count(im.multiBuf.String(), "\n") + 1
		inner.WriteString(inputHintStyle.Render(
			fmt.Sprintf("multiline (%d lines) · Enter send · Esc cancel", lines)))
		inner.WriteString("\n")
	}

	inner.WriteString(im.input.View())

	return inputBoxStyle.Width(im.width - 4).Render(inner.String())
}
