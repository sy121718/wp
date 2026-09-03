// Package tabs — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / radio 与标签 HTML 预计算
// 保留在 Go，HTML 拼装交给 tabs.jet 模板（radio 在 nav 前，children 递归 include）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package tabs

import (
	"fmt"
	"html"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出页签样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// TabView 单个页签的预计算 HTML（供 tabs.jet 模板使用）。
type TabView struct {
	// RadioHTML 完整 <input type="radio" ...>（含 checked 标记），原样输出。
	RadioHTML string
	// LabelHTML 完整 <label for="..." role="tab">文案</label>（文案已转义），原样输出。
	LabelHTML string
}

// View tabs 渲染视图数据（供 tabs.jet 模板使用）。
type View struct {
	// Tabs 页签列表（与 children 面板一一对应，顺序一致）。
	Tabs []TabView
	// Vertical 竖向布局。
	Vertical bool
}

// BuildView 生成页签渲染视图：radio + label HTML 预计算（radio 置于 nav 之前，
// CSS 兄弟选择器 ~ 依赖此顺序）。children 面板的递归渲染由 nodeView 层驱动。
func BuildView(node *core.Node, p *Props) View {
	id := html.EscapeString(node.ID)
	tabs := make([]TabView, 0, len(p.Tabs))
	for i, t := range p.Tabs {
		tv := TabView{}
		// radio：id 与 name 带节点 ID 前缀 + 序号；首个默认选中。
		tv.RadioHTML = `<input type="radio" class="wp-tabs-radio" id="wp-tabs-` + id + `-` + fmt.Sprintf("%d", i) +
			`" name="wp-tabs-` + id + `"`
		if i == 0 {
			tv.RadioHTML += ` checked`
		}
		tv.RadioHTML += `>`
		// label：for 指向对应 radio，文案转义。
		tv.LabelHTML = `<label for="wp-tabs-` + id + `-` + fmt.Sprintf("%d", i) + `" role="tab">` +
			html.EscapeString(t.Label) + `</label>`
		tabs = append(tabs, tv)
	}
	return View{Tabs: tabs, Vertical: p.Vertical}
}
