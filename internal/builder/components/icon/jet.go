// Package icon — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 图标路径选取保留在 Go，
// HTML 拼装交给 icon.jet 模板。render 函数保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package icon

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出图标样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View icon 渲染视图数据（供 icon.jet 模板使用）。
type View struct {
	// IconSVG 图标内部元素（path/circle/line，不含 <svg> 包裹），原样输出。
	IconSVG string
}

// BuildView 生成图标渲染视图：按 IconName 选取白名单 SVG（空或未知兜底 star）。
func BuildView(p *Props) View {
	name := p.IconName
	if name == "" {
		name = defaultIconName
	}
	path, ok := builtinIcons[name]
	if !ok {
		path = builtinIcons[defaultIconName]
	}
	return View{IconSVG: path}
}
