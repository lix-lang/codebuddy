package tui

// TabsModel 多标签页（占位，Phase 2 实现）
type TabsModel struct{}

// NewTabsModel 创建多标签页
func NewTabsModel() TabsModel {
	return TabsModel{}
}

// View 渲染标签页
func (t TabsModel) View() string {
	return ""
}
