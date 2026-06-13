package tui

// TabsModel 多标签页（占位）
type TabsModel struct{}

// NewTabsModel 创建标签页模型
func NewTabsModel() TabsModel {
	return TabsModel{}
}

// View 渲染标签页
func (t TabsModel) View() string {
	return ""
}
