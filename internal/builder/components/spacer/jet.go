// Package spacer — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 spacer.jet 模板（仅单层 div，无额外预计算）。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package spacer

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出间隔组件样式编译（与 render 内部三端高度 CSS 逻辑一致）。
// 说明：spacer 无独立 compileCSS 私有函数，render 内联三端高度写入 CSSBuckets，
// 此处复刻同一逻辑保证新路径 CSS 字节与旧路径一致。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if v := p.Height.Desktop; v != "" {
		b.Add(core.BreakpointDesktop, sel, []string{"height: " + v})
	}
	if v := p.Height.Tablet; v != "" {
		b.Add(core.BreakpointTablet, sel, []string{"height: " + v})
	}
	if v := p.Height.Mobile; v != "" {
		b.Add(core.BreakpointMobile, sel, []string{"height: " + v})
	}
}

// View spacer 渲染视图数据（空——模板仅依赖 nodeView 通用字段 Classes/CustomID）。
type View struct{}

// BuildView 生成 spacer 渲染视图（无内容预计算）。
func BuildView(p *Props) View {
	return View{}
}
