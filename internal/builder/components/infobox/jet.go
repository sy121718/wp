// Package infobox — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / body 内容（图标·媒体图·标题·文本）
// 预计算保留在 Go，HTML 拼装交给 infobox.jet 模板（链接/按钮化/纯卡片三分支）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package infobox

import (
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出信息框样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View infobox 渲染视图数据（供 infobox.jet 模板使用）。
type View struct {
	// BodyHTML 图标/媒体图 + 副标题 + 标题 + 文本（已转义，原样输出）。
	BodyHTML string
	// HasLink 是否有链接（整卡链接或按钮化）。
	HasLink bool
	// HasBtn 是否按钮化（Link 非空且 BtnText 非空）。
	HasBtn bool
	// Link 链接地址（模板输出时由 Jet 默认转义）。
	Link string
	// BtnText 按钮文字（模板输出时由 Jet 默认转义）。
	BtnText string
}

// BuildView 生成信息框渲染视图：body 预计算 + 链接/按钮化分支标志。
func BuildView(p *Props) View {
	var body strings.Builder
	if p.MediaImage != "" {
		body.WriteString(`<span class="wp-infobox-media">`)
		body.WriteString(`<img src="`)
		body.WriteString(html.EscapeString(p.MediaImage))
		body.WriteString(`" alt="" loading="lazy">`)
		body.WriteString(`</span>`)
	} else if p.Icon != "" {
		body.WriteString(`<span class="wp-infobox-icon">`)
		if svg, ok := core.IconSVG(p.Icon); ok {
			body.WriteString(svg)
		}
		body.WriteString(`</span>`)
	}
	if p.Subtitle != "" {
		body.WriteString(`<span class="wp-infobox-subtitle">`)
		body.WriteString(html.EscapeString(p.Subtitle))
		body.WriteString(`</span>`)
	}
	if p.Title != "" {
		tag := p.TitleTag
		if tag == "" {
			tag = "h3"
		}
		body.WriteString("<" + tag + ` class="wp-infobox-title">`)
		body.WriteString(html.EscapeString(p.Title))
		body.WriteString("</" + tag + ">")
	}
	if p.Text != "" {
		body.WriteString(`<div class="wp-infobox-text">`)
		body.WriteString(html.EscapeString(p.Text))
		body.WriteString(`</div>`)
	}

	v := View{BodyHTML: body.String()}
	if strings.TrimSpace(p.Link) != "" {
		v.HasLink = true
		v.Link = p.Link
		if strings.TrimSpace(p.BtnText) != "" {
			v.HasBtn = true
			v.BtnText = p.BtnText
		}
	}
	return v
}
