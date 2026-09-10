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
	// TitleTag 标题标签名（h2~h5），默认 h3：由页面结构决定，避免标题跳级。
	TitleTag string
	// Title 标题（模板输出时由 Jet 默认转义）。
	Title string
	// Text 正文（已由 core.RichTextHTML 处理：富文本白名单清洗 / 存量纯文本段落化，
	// 模板侧 unsafe 原样输出）。
	Text string
	// HasButton 是否有按钮（ButtonText 与 ButtonLink 均非空）。
	HasButton bool
	// ButtonText 按钮文字。
	ButtonText string
	// ButtonLink 按钮链接。
	ButtonLink string

	// Loading 组件级加载策略三态原值（空=继承主题，由 ApplyImageLoading 解析）。
	Loading string
	// IsEager 立即加载（ApplyImageLoading 后有效）。
	IsEager bool
	// Skeleton 懒加载骨架屏（ApplyImageLoading 后有效）。
	Skeleton bool
	// Class <img> 的 class（骨架类并入后；空则不输出 class 属性）。
	Class string
	// FetchPriority 资源提示原值（空=不输出属性）。
	FetchPriority string
	// FetchHigh / FetchLow 资源提示（ApplyImageLoading 后有效）。
	FetchHigh bool
	FetchLow  bool
}

// BuildView 生成卡片渲染视图：图片/按钮可选分支，文本字段直通。
func BuildView(p *Props) View {
	// 标题标签白名单：只允许 h2~h5（h1 由页面标题承担，一个页面只有一个 h1）。
	titleTag := p.TitleTag
	switch titleTag {
	case "h2", "h3", "h4", "h5":
	default:
		titleTag = "h3"
	}
	return View{
		TitleTag:      titleTag,
		HasImage:      p.ImageSrc != "",
		ImageSrc:      p.ImageSrc,
		Title:         p.Title,
		Text:          core.RichTextHTML(p.Text),
		HasButton:     p.ButtonText != "" && p.ButtonLink != "",
		ButtonText:    p.ButtonText,
		ButtonLink:    p.ButtonLink,
		Loading:       p.Loading,
		FetchPriority: p.FetchPriority,
	}
}

// ApplyImageLoading 按主题「图片管理」默认解析加载三态（实现 core.ImageLoadingAware）。
// 返回 true 表示需要骨架屏 CSS，由渲染层统一输出（CSSBuckets 去重，多图不重复）。
func (v *View) ApplyImageLoading(d core.ImageDefaults) bool {
	attrs := core.ResolveImageLoading(v.Loading, d)
	v.IsEager, v.Skeleton = attrs.IsEager, attrs.Skeleton
	v.Class = core.ImageSkeletonClass(v.Class, attrs.Skeleton)
	fp := core.ResolveFetchPriority(v.FetchPriority)
	v.FetchHigh, v.FetchLow = fp.High, fp.Low
	return attrs.Skeleton
}
