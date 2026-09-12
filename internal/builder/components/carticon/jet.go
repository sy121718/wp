package carticon

// Package carticon — Jet 渲染路径（视图组装）。
//
// 图标 SVG 取 core 内置图标库；片段地址按「工程 + 语言」编好交给模板 ——
// 让模板自己拼 URL 就等于把「片段端点长什么样」散到模板里，
// 将来端点改名要满世界找。

import (
	"net/url"
	"strings"

	"go_wp/internal/builder/core"
)

// View 购物车图标渲染视图。
type View struct {
	// Mode 展示形态。
	Mode string
	// IconSVG 内置图标的内联 SVG（完整 <svg>，模板原样输出）。
	IconSVG string
	// Label 标签文字（同时是无障碍标签）。
	Label string
	// ShowLabel 图标旁是否显示文字。
	ShowLabel bool
	// ShowCount 是否显示件数角标。
	ShowCount bool
	// UseDetails 外壳用 <details>（dropdown / drawer / modal）还是纯链接（hover）。
	//
	// <details> 是原生可展开元素：无 JS 也能开合、键盘可达、触屏可用 ——
	// 不自己实现开合状态，那是这几类浮层最容易写错的地方（焦点陷阱、点外部不关）。
	UseDetails bool
	// CartURL 购物车页的线上路径（槽位 cart）；空 = 这个站还没指定购物车页。
	CartURL string
	// HasCartURL CartURL 是否可用（模板据此决定图标是否可点）。
	HasCartURL bool
	// CartViewURL 购物车内容片段地址（打开浮层时现拉）。
	CartViewURL string
	// SummaryURL 件数角标片段地址。
	SummaryURL string
	// Notice 无法渲染时的提示（缺站点工程 id）。空表示正常。
	Notice string
}

// CompileCSS 导出样式编译。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// BuildView 生成购物车图标视图。
//
// projectID 来自构建上下文（片段地址要带它），lang 决定要不要带语言参数，
// cartURL 来自系统页面槽位（无 JS 时的兜底链接目标）。
func BuildView(p *Props, projectID, lang, cartURL string) View {
	mode := effectiveMode(p)
	view := View{
		Mode:       mode,
		IconSVG:    iconSVG(p),
		Label:      effectiveLabel(p),
		ShowLabel:  p.ShowLabel,
		ShowCount:  p.ShowCount,
		UseDetails: mode != ModeHover,
		CartURL:    strings.TrimSpace(cartURL),
	}
	view.HasCartURL = view.CartURL != ""
	if strings.TrimSpace(projectID) == "" {
		// 没有工程 id 就取不到购物车（片段端点按工程定位）。留一句可见提示，
		// 而不是渲染一个点开永远空着的图标 —— 那种「看起来正常但不工作」最难查。
		view.Notice = "购物车暂不可用（未取到站点工程）"
		return view
	}
	q := url.Values{}
	q.Set("projectId", strings.TrimSpace(projectID))
	// 语言只在非空时带上：单语言站点带上一个空 lang 参数会让片段多做一次无用判断。
	if l := strings.TrimSpace(lang); l != "" {
		q.Set("lang", l)
	}
	encoded := q.Encode()
	view.CartViewURL = cartViewPath + "?" + encoded
	view.SummaryURL = cartSummaryPath + "?" + encoded
	return view
}

// ApplyI18n 按当前语言填充标签文字（实现 core.I18nAware）。
//
// 只在作者**没有自定义**文案时生效：自定义文案属于内容，走 Translatable 那条链路。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if v.Label != textFallbackLabel {
		return // 作者自定义的文案不在这里翻译
	}
	if text == nil {
		v.Label = textFallbackLabel
		return
	}
	v.Label = text(TextKeyLabel, textFallbackLabel)
}

// 界面文案键（多语言 P5b）。
const (
	TextKeyLabel = "site.component.cartIcon.label"
	// textFallbackLabel 标签文字的中文兜底。
	textFallbackLabel = defaultLabel
)
