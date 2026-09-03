// Package divider — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 嵌入元素预计算保留在 Go，
// HTML 拼装交给 divider.jet 模板（纯线 hr / Flex 三段结构）。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package divider

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出分割线样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View divider 渲染视图数据（供 divider.jet 模板使用）。
type View struct {
	// IsInset 是否有嵌入元素（Kind != "" && Kind != "none"）。
	IsInset bool
	// IsText 嵌入类型为文本（否则为图标）。
	IsText bool
	// InsetText 嵌入文本（模板输出时由 Jet 默认转义）。
	InsetText string
	// InsetIconHTML 嵌入图标完整 SVG（空则无，原样输出）。
	InsetIconHTML string
}

// BuildView 生成分割线渲染视图：纯线 / 文本嵌入 / 图标嵌入（与 render 输出结构一致）。
func BuildView(p *Props) View {
	v := View{IsInset: p.Inset.Kind != InsetNone && p.Inset.Kind != ""}
	if v.IsInset {
		if p.Inset.Kind == InsetText {
			v.IsText = true
			v.InsetText = p.Inset.Text
		} else if path, ok := builtinInsetIcons[p.Inset.IconName]; ok {
			v.InsetIconHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">` +
				path + `</svg>`
		}
	}
	return v
}
