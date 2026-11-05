package core

// NodeView 是组件渲染后的中立视图，供模板组合使用。
//
// 它只承载展示事实，不引用工具私有类型；通用组件与业务扩展都返回它。
type NodeView struct {
	Type     string
	Template string
	NodeID   string
	Classes  string
	CustomID string
	TopLevel bool
	Props    any
	Children []*NodeView

	V any

	Tag         string
	Attrs       string
	Text        string
	IconPrefix  string
	IconSuffix  string
	ShapeTop    string
	ShapeBottom string
	BgSlides    []string
}

// ViewBuilder 是组件的可选视图构建入口。
//
// 组件在 core.Register 时若实现了它，nodeView 骨架会调用它来构建视图；
// 没有实现则由通用骨架按描述/模板渲染。业务组件在应用扩展里实现并注册。
type ViewBuilder interface {
	BuildNodeView(node *Node, topLevel bool, ctx *RenderContext) (*NodeView, error)
}
