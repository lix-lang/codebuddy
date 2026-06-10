package tui

// DiffModel Diff 查看器（占位，Phase 2 实现）
type DiffModel struct{}

// NewDiffModel 创建 Diff 查看器
func NewDiffModel() DiffModel {
	return DiffModel{}
}

// View 渲染 Diff 视图
func (d DiffModel) View() string {
	return ""
}
