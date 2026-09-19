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
	// MediaAlt 媒体图替代文本（空 = 装饰性图片，输出 alt=""）。
	MediaAlt string
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
	// Text 描述文本（已由 core.RichTextHTML 处理：富文本白名单清洗 / 存量纯文本段落化，
	// 模板侧 unsafe 原样输出）。
	Text string

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

// BuildView 生成信息框渲染视图：body 内容字段准备 + 链接/按钮化分支标志。
//
// siteLink 站内链接本地化器（审计 I18N-015，可空）：作者手填的站内路径要按当前语言加前缀，
// 否则英文站点上的信息框点过去会跳回默认语言版本。
func BuildView(p *Props, siteLink func(string) string) View {
	v := View{}

	// 媒体图优先于图标（与旧 Render 一致）。
	if p.MediaImage != "" {
		v.HasMedia = true
		v.MediaSrc = p.MediaImage
		v.MediaAlt = p.MediaAlt
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
	v.Text = core.RichTextHTML(p.Text)
	v.Loading = p.Loading
	v.FetchPriority = p.FetchPriority

	if strings.TrimSpace(p.Link) != "" {
		v.HasLink = true
		v.Link = core.SiteLinkOrSame(siteLink, p.Link)
		if strings.TrimSpace(p.BtnText) != "" {
			v.HasBtn = true
			v.BtnText = p.BtnText
		}
	}
	return v
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
