// Package accordion — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 折叠项标题与展开态预计算
// 保留在 Go，HTML 拼装交给 accordion.jet 模板（details/summary + children 递归 include）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package accordion

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出手风琴样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// ItemView 单个折叠项视图（供 accordion.jet 模板使用）。
type ItemView struct {
	// Title 折叠项标题（模板输出时由 Jet 默认转义）。
	Title string
	// Open 默认展开。
	Open bool
}

// View accordion 渲染视图数据（供 accordion.jet 模板使用）。
type View struct {
	// Items 折叠项标题列表（与 children 一一对应，顺序一致）。
	Items []ItemView
	// OneOpen 严格手风琴模式（data-one-open="1"）。
	OneOpen bool
	// Borderless 无边框样式。
	Borderless bool
}

// BuildView 生成手风琴渲染视图：折叠项标题/展开态预计算（与 Render 输出结构一致）。
// children 的递归渲染由 nodeView 层驱动（Items 与 Children 顺序一一对应）。
func BuildView(p *Props) View {
	items := make([]ItemView, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, ItemView{Title: it.Title, Open: it.Open})
	}
	return View{Items: items, OneOpen: p.OneOpen, Borderless: p.Borderless}
}
