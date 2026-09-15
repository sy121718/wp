// Package button — Jet 渲染路径辅助导出（Phase 0 样板）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 渲染数据计算保留在 Go，
// HTML 拼装交给 button.jet 模板。render 函数保持不变（旧输出），本文件只做
// 最小导出与等价的数据准备，供 builder/jetview.go 的 nodeView 转换层复用。
package button

import (
	"fmt"
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出按钮样式编译（复用 render 内部的 compileCSS，CSS 字节与旧路径一致）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View button 渲染视图数据（供 button.jet 模板使用）。
type View struct {
	// Tag 语义标签：a（跳转）或 button（弹窗触发）。
	Tag string
	// Attrs 前导空格 + 属性串（href/target/rel/data-modal-open，均已转义）。
	Attrs string
	// Text 按钮文本（模板输出时由 Jet 默认转义）。
	Text string
	// IconPrefix / IconSuffix 图标渲染片段（空则无，原样输出）：
	//   - builtin：内置图标内部元素（path 片段，不含 <svg> 包裹）；
	//   - media：已转义的媒体图标地址。
	// <svg>/<img> 骨架与动态属性（class/size/type）由 button.jet 模板经 .Props.Icon 渲染。
	IconPrefix string
	IconSuffix string
	// Loading 组件级加载策略三态原值（空=继承主题，由 ApplyImageLoading 解析）。
	Loading string
	// IsEager 立即加载（ApplyImageLoading 后有效；仅媒体库图标 <img> 生效）。
	IsEager bool
	// Skeleton 懒加载骨架屏（ApplyImageLoading 后有效）。
	Skeleton bool
	// Class 附加 class（仅骨架类；模板拼在 "bt-icon" 之后，空则不追加）。
	Class string
	// FetchPriority 资源提示原值（空=不输出属性）。
	FetchPriority string
	// FetchHigh / FetchLow 资源提示（ApplyImageLoading 后有效）。
	FetchHigh bool
	FetchLow  bool
}

// BuildView 生成按钮渲染视图：标签选择 + 链接协议 + 图标（与 render 输出结构一致）。
func BuildView(p *Props, content core.ContentResolver, siteLink func(string) string) (View, error) {
	fragment, err := buildIconFragment(p)
	if err != nil {
		return View{}, err
	}
	tag, attrs, err := buildAttrs(p, content, siteLink)
	if err != nil {
		return View{}, err
	}
	v := View{Tag: tag, Attrs: attrs, Text: p.Text, Loading: p.Loading, FetchPriority: p.FetchPriority}
	if p.Icon != nil {
		// top 与 prefix 同用前缀位、bottom 与 suffix 同用后缀位，
		// 上下排布由编译端 flex-direction: column 实现（见 CompileCSS）。
		switch p.Icon.Position {
		case "suffix", "bottom":
			v.IconSuffix = fragment
		default:
			v.IconPrefix = fragment
		}
	}
	return v, nil
}

// ApplyImageLoading 按主题「图片管理」默认解析加载三态（实现 core.ImageLoadingAware）。
// 返回 true 表示需要骨架屏 CSS，由渲染层统一输出（CSSBuckets 去重，多图不重复）。
func (v *View) ApplyImageLoading(d core.ImageDefaults) bool {
	attrs := core.ResolveImageLoading(v.Loading, d)
	v.IsEager, v.Skeleton = attrs.IsEager, attrs.Skeleton
	// Class 只放附加类：模板里 <img class="bt-icon{{ ... }}"> 追加，避免覆盖 .bt-icon 样式。
	v.Class = core.ImageSkeletonClass("", attrs.Skeleton)
	fp := core.ResolveFetchPriority(v.FetchPriority)
	v.FetchHigh, v.FetchLow = fp.High, fp.Low
	return attrs.Skeleton
}

// buildIconFragment 计算按钮图标渲染片段（去 <svg>/<img> 骨架，骨架由 button.jet 模板渲染）。
//   - 无图标：返回空串；
//   - builtin：返回内置图标内部元素（path 片段）；
//   - media：返回已转义的媒体图标地址（模板输出时经 unsafe 原样注入 src）。
func buildIconFragment(p *Props) (string, error) {
	if p.Icon == nil {
		return "", nil
	}
	if p.Icon.Source == "builtin" {
		path, ok := builtinIcons[p.Icon.Name]
		if !ok {
			return "", fmt.Errorf("无效的内置图标: %q", p.Icon.Name)
		}
		return path, nil
	}
	// 媒体库/外链图标：URL 直引 img（构建期零解析，不内联 SVG 源码）。
	return html.EscapeString(p.Icon.URL), nil
}

// buildAttrs 标签与属性选择（与 render 内联逻辑逐字一致，保持旧输出不变）。
// link 站内链接本地化器（审计 I18N-015，可空）：作者手填的 /shop 这类站内路径要按当前
// 语言加前缀，否则英文站点上的按钮会跳回默认语言版本 —— 页面看起来正常，只是点过去语言变了。
// 外链 / 锚点 / 协议相对地址由 core.ResolveSiteLink 自行放行，这里不做判断。
func buildAttrs(p *Props, content core.ContentResolver, siteLink func(string) string) (tag, attrs string, err error) {
	if siteLink == nil {
		siteLink = func(s string) string { return s }
	}
	tag = "a"
	switch p.Action {
	case ActionModal:
		tag = "button"
		attrs = ` type="button" data-modal-open="` + html.EscapeString(p.Value) + `"`
	case ActionLink:
		if content == nil {
			return "", "", fmt.Errorf("编译上下文缺少内容解析器，无法解析动态链接")
		}
		// 绑定缺失必须在此拦截：validateExtra 只在「文本与绑定双空」时报错，
		// 因此 {"text":"x","action":"link"}（Binding 为 nil）能通过校验抵达这里，
		// 直接取 p.Binding.Field 会 nil 解引用并 panic 掉整个构建 worker。
		if p.Binding == nil || p.Binding.Field == "" {
			return "", "", fmt.Errorf("动态链接动作缺少绑定字段（binding.field），请配置绑定或改用其他动作")
		}
		v, e := content.ResolveString(p.Binding.Field)
		if e != nil {
			return "", "", fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, e)
		}
		if v == "" {
			return "", "", fmt.Errorf("绑定字段 %q 为空，无 fallback 兜底", p.Binding.Field)
		}
		// 绑定值协议白名单校验：CMS 内容属不受信输入，渲染 href 前必须过协议校验，
		// 拒绝 javascript:/data: 等危险协议。校验失败时安全降级为无 href（不可点击），
		// 不阻断整页编译（绑定值是 CMS 内容，不应让整页编译失败）。
		if !core.IsSafeURL(v) {
			return tag, "", nil
		}
		attrs = ` href="` + html.EscapeString(v) + `"` // CMS 值不本地化：语义是内容作者掌握的完整地址
	case ActionAnchor:
		attrs = ` href="#` + html.EscapeString(p.Value) + `"`
	case ActionNative, ActionInternal:
		attrs = ` href="` + html.EscapeString(siteLink(p.Value)) + `"`
	default: // external
		attrs = ` href="` + html.EscapeString(p.Value) + `"`
		relParts := []string{}
		if p.Target == "blank" {
			attrs += ` target="_blank"`
			relParts = append(relParts, "noopener", "noreferrer")
		}
		if p.Rel == "nofollow" || p.Rel == "sponsored" {
			relParts = append(relParts, p.Rel)
		}
		if len(relParts) > 0 {
			attrs += ` rel="` + strings.Join(relParts, " ") + `"`
		}
	}
	return tag, attrs, nil
}
