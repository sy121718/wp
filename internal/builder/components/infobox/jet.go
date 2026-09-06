// Package infobox — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 infobox.jet 模板（链接/按钮化/纯卡片三分支 + body 结构）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package infobox

import (
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出信息框样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View infobox 渲染视图数据（供 infobox.jet 模板使用）。
type View struct {
	// HasLink 是否有链接（整卡链接或按钮化）。
	HasLink bool
	// HasBtn 是否按钮化（Link 非空且 BtnText 非空）。
	HasBtn bool
	// Link 链接地址（模板输出时由 Jet 默认转义）。
	Link string
	// BtnText 按钮文字（模板输出时由 Jet 默认转义）。
	BtnText string

	// --- body 内容字段 ---
	// HasMedia 是否有媒体图（优先于 Icon）。
	HasMedia bool
	// MediaSrc 媒体图地址（模板输出时由 Jet 默认转义）。
	MediaSrc string
	// HasIcon 是否有图标（无媒体图且 Icon 非空）。
	HasIcon bool
	// IconSVG 内置图标内联 SVG（白名单，模板 | unsafe 原样输出）。
	IconSVG string
	// Subtitle 副标题（模板输出时由 Jet 默认转义）。
	Subtitle string
	// Title 标题（模板输出时由 Jet 默认转义）。
	Title string
	// TitleTag 标题标签（默认 h3，模板输出时由 Jet 默认转义）。
	TitleTag string
	// Text 描述文本（模板输出时由 Jet 默认转义）。
	Text string
}

// BuildView 生成信息框渲染视图：body 内容字段准备 + 链接/按钮化分支标志。
func BuildView(p *Props) View {
	v := View{}

	// 媒体图优先于图标（与旧 Render 一致）。
	if p.MediaImage != "" {
		v.HasMedia = true
		v.MediaSrc = p.MediaImage
	} else if p.Icon != "" {
		v.HasIcon = true
		if svg, ok := core.IconSVG(p.Icon); ok {
			v.IconSVG = svg
		}
	}
	v.Subtitle = p.Subtitle
	v.Title = p.Title
	v.TitleTag = p.TitleTag
	if v.TitleTag == "" {
		v.TitleTag = "h3"
	}
	v.Text = p.Text

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
