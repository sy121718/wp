// Package socialbuttons — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成（品牌色按 brandOrder 有序）/
// 平台 SVG 预计算保留在 Go，HTML 拼装交给 socialbuttons.jet 模板。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package socialbuttons

import (
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出社交按钮样式编译（复用 Render 内部的 compileCSS，品牌色有序）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// ItemView 单个社交按钮渲染视图（供 socialbuttons.jet 模板使用）。
type ItemView struct {
	// Platform 平台标识（aria-label，模板输出时由 Jet 默认转义）。
	Platform string
	// URL 跳转地址（模板输出时由 Jet 默认转义）。
	URL string
	// InnerHTML 内联 SVG 或首字母兜底 span（原样输出）。
	InnerHTML string
}

// View socialbuttons 渲染视图数据（供 socialbuttons.jet 模板使用）。
type View struct {
	// Color 配色模式：brand / mono / custom。
	Color string
	// Items 按钮列表（repeater）。
	Items []ItemView
}

// BuildView 生成社交按钮渲染视图：平台 SVG 预计算 + 品牌色 class 取值。
func BuildView(p *Props) View {
	color := p.Color
	if color == "" {
		color = ColorBrand
	}
	items := make([]ItemView, 0, len(p.Items))
	for _, it := range p.Items {
		iv := ItemView{Platform: it.Platform, URL: it.URL}
		if svg, ok := platformIcons[it.Platform]; ok {
			iv.InnerHTML = svg
		} else {
			// 无专属 SVG 的平台：首字母圆形兜底（品牌色已按平台映射）。
			iv.InnerHTML = `<span class="wp-social-fallback">` +
				html.EscapeString(strings.ToUpper(it.Platform[:1])) + `</span>`
		}
		items = append(items, iv)
	}
	return View{Color: string(color), Items: items}
}
