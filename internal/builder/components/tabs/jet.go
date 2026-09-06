// Package tabs — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 tabs.jet 模板（radio 在 nav 前，children 递归 include）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package tabs

import (
	"strconv"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出页签样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// TabView 单个页签的渲染视图数据（供 tabs.jet 模板使用）。
type TabView struct {
	// ID 页签 radio 的完整 id（wp-tabs-<节点ID>-<序号>），模板输出时由 Jet 默认转义。
	ID string
	// Name 页签 radio 的 name（wp-tabs-<节点ID>），模板输出时由 Jet 默认转义。
	Name string
	// Checked 是否默认选中（首个为 true）。
	Checked bool
	// Label 页签文案（模板输出时由 Jet 默认转义）。
	Label string
}

// View tabs 渲染视图数据（供 tabs.jet 模板使用）。
type View struct {
	// Tabs 页签列表（与 children 面板一一对应，顺序一致）。
	Tabs []TabView
	// Vertical 竖向布局。
	Vertical bool
}

// BuildView 生成页签渲染视图：radio id/name 预计算 + 默认选中标记（首个选中），
// children 面板的递归渲染由 nodeView 层驱动（tabs.jet 内 include）。
func BuildView(node *core.Node, p *Props) View {
	tabs := make([]TabView, 0, len(p.Tabs))
	for i, t := range p.Tabs {
		tabs = append(tabs, TabView{
			ID:      "wp-tabs-" + node.ID + "-" + strconv.Itoa(i),
			Name:    "wp-tabs-" + node.ID,
			Checked: i == 0,
			Label:   t.Label,
		})
	}
	return View{Tabs: tabs, Vertical: p.Vertical}
}
