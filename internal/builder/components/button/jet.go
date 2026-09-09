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
	// Attrs 前导空格 + 属性串（href/target/rel/data-modal-target，均已转义）。
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
func BuildView(p *Props, content core.ContentResolver) (View, error) {
	fragment, err := buildIconFragment(p)
	if err != nil {
		return View{}, err
	}
	tag, attrs, err := buildAttrs(p, content)
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
func buildAttrs(p *Props, content core.ContentResolver) (tag, attrs string, err error) {
	tag = "a"
	switch p.Action {
	case ActionModal:
		tag = "button"
		attrs = ` type="button" data-modal-target="` + html.EscapeString(p.Value) + `"`
	case ActionLink:
		if content == nil {
			return "", "", fmt.Errorf("编译上下文缺少内容解析器，无法解析动态链接")
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
		attrs = ` href="` + html.EscapeString(v) + `"`
	case ActionAnchor:
		attrs = ` href="#` + html.EscapeString(p.Value) + `"`
	case ActionNative, ActionInternal:
		attrs = ` href="` + html.EscapeString(p.Value) + `"`
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
