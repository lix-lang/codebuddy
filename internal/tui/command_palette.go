package tui

// CommandPaletteModel 命令面板（占位，Ctrl+K 触发）
type CommandPaletteModel struct{}

// NewCommandPaletteModel 创建命令面板模型
func NewCommandPaletteModel() CommandPaletteModel {
	return CommandPaletteModel{}
}

// View 渲染命令面板
func (c CommandPaletteModel) View() string {
	return ""
}
