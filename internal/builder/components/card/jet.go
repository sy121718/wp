// Package card — Jet 渲染路径辅助导出。
//
// 与 compileCSS 并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 card.jet 模板（article + img + h3 + p + a）。
// 本文件只做最小导出与等价的数据准备。
package card

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出卡片样式编译（复用 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View card 渲染视图数据（供 card.jet 模板使用）。
type View struct {
	// HasImage 是否有顶部图片。
	HasImage bool
	// ImageSrc 图片地址（模板输出时由 Jet 默认转义）。
	ImageSrc string
	// Title 标题（模板输出时由 Jet 默认转义）。
	Title string
	// Text 正文（模板输出时由 Jet 默认转义）。
	Text string
	// HasButton 是否有按钮（ButtonText 与 ButtonLink 均非空）。
	HasButton bool
	// ButtonText 按钮文字。
	ButtonText string
	// ButtonLink 按钮链接。
	ButtonLink string
}

// BuildView 生成卡片渲染视图：图片/按钮可选分支，文本字段直通。
func BuildView(p *Props) View {
	return View{
		HasImage:   p.ImageSrc != "",
		ImageSrc:   p.ImageSrc,
		Title:      p.Title,
		Text:       p.Text,
		HasButton:  p.ButtonText != "" && p.ButtonLink != "",
		ButtonText: p.ButtonText,
		ButtonLink: p.ButtonLink,
	}
}
