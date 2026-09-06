// Package badge — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 badge.jet 模板（单层 span + 文字）。render 函数保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package badge

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出徽章样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View badge 渲染视图数据（供 badge.jet 模板使用）。
type View struct {
	// Text 徽章文字（模板输出时由 Jet 默认转义）。
	Text string
}

// BuildView 生成徽章渲染视图：仅透传文字。
func BuildView(p *Props) View {
	return View{Text: p.Text}
}
