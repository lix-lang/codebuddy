package tui

// DiffModel Diff 查看器（占位）
type DiffModel struct{}

// NewDiffModel 创建 Diff 查看器模型
func NewDiffModel() DiffModel {
	return DiffModel{}
}

// View 渲染 Diff 查看器
func (d DiffModel) View() string {
	return ""
}
