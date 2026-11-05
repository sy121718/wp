package core

import "sync"

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

	SlotFrame *SlotFrame
}

// ViewBuilder 是组件的可选视图构建入口。
//
// 组件在 core.Register 时若实现了它，nodeView 骨架会调用它来构建视图；
// 没有实现则由通用骨架按描述/模板渲染。业务组件在应用扩展里实现并注册。
type ViewBuilder interface {
	BuildNodeView(node *Node, topLevel bool, ctx *RenderContext) (*NodeView, error)
}

// SlotFrame 是结构槽位的展示事实（画布槽位帧用）。
type SlotFrame struct {
	Slot    string
	Ref     string
	RefKind string
	NodeID  string
}

// NodeViewFunc 是业务组件的视图构建函数形状。
type NodeViewFunc func(node *Node, topLevel bool, ctx *RenderContext) (*NodeView, error)

var (
	viewBuildersMu sync.RWMutex
	viewBuilders   = map[string]NodeViewFunc{}
)

// RegisterNodeView 登记业务组件的视图构建函数。
//
// 应用装配时调用（业务扩展在 init 或显式注册里提供），
// 工具内核按类型查表，不反向 import 任何业务扩展。
func RegisterNodeView(typeName string, fn NodeViewFunc) {
	if typeName == "" || fn == nil {
		panic("core.RegisterNodeView: 类型名与构建函数都不能为空")
	}
	viewBuildersMu.Lock()
	defer viewBuildersMu.Unlock()
	viewBuilders[typeName] = fn
}

// NodeViewDispatch 是「按类型分发到骨架」的回调，由工具内核在装配时提供。
//
// 业务扩展在 BuildNodeView 里需要渲染子节点时用它，避免反向 import 工具内核。
var NodeViewDispatch NodeViewFunc

// NodeViewBuilderOf 返回该类型的已登记构建函数。
func NodeViewBuilderOf(typeName string) (NodeViewFunc, bool) {
	viewBuildersMu.RLock()
	defer viewBuildersMu.RUnlock()
	fn, ok := viewBuilders[typeName]
	return fn, ok
}
