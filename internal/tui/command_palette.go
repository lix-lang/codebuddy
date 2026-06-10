package tui

// CommandPaletteModel 命令面板（占位，Phase 2 实现）
type CommandPaletteModel struct{}

// NewCommandPaletteModel 创建命令面板
func NewCommandPaletteModel() CommandPaletteModel {
	return CommandPaletteModel{}
}

// View 渲染命令面板
func (c CommandPaletteModel) View() string {
	return ""
}
