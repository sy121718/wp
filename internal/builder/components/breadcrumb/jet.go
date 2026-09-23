// breadcrumb — Jet 渲染路径辅助导出。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成保留在 Go，HTML 拼装交给
// breadcrumb.jet 模板。层级数据的唯一来源在本文件的 BuildView / ApplyI18n：
// 可见项与（可选的）BreadcrumbList 结构化数据都由同一份 Items 序列化出来。
package breadcrumb

import (
	"encoding/json"
	"net/url"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/seo"
)

// CompileCSS 导出面包屑样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// ItemView 单个层级项的渲染视图。
type ItemView struct {
	// Label 展示名（模板输出时由 Jet 默认转义）。
	Label string
	// URL 链接（空 = 纯文本项，模板不输出 a 标签）。
	URL string
	// Current 是否为末项（当前页面）：输出 is-current 与 aria-current。
	Current bool
	// Separator 该项之后的分隔符（末项为空串 = 不输出分隔符）。
	Separator string
}

// View 面包屑渲染视图（供 breadcrumb.jet 模板使用）。
type View struct {
	Items []ItemView
	// Separator 分隔符原文（作者未填时取缺省 /）。
	Separator string
	// Visible 是否有可显示层级：首页自身（无路径段）不渲染面包屑。
	Visible bool
	// Label 面包屑容器 aria-label（构建期按当前语言填充，多语言 P4）。
	Label string
	// JSONLD 与可见项同源的 BreadcrumbList 脚本（jsonLd 未开时为空串）。
	JSONLD string

	// homeIndex 首页项在 Items 中的下标（-1 = 无首页项；手填层级时不介入）。
	homeIndex int
	// homeFromDefault 首页名是否来自缺省（作者没填 homeLabel），决定是否参与取词。
	homeFromDefault bool
	// jsonLDEnabled 是否输出组件自带的结构化数据（作者显式打开）。
	jsonLDEnabled bool
}

// 访客面组件文案 key：site.component.{type}.{prop}（docs/06-D §10.3）。
const (
	// TextKeyLabel 面包屑容器 aria-label 的词条 key。
	TextKeyLabel = "site.component.breadcrumb.label"
	// TextKeyHome 首页项文案的词条 key —— 与产物 head 的 JSON-LD 面包屑首项
	// 取同一条词条（builder.go 的 breadcrumbHome），两处共用一份词条才不至于各说各话。
	TextKeyHome = "site.breadcrumb.home"
)

// 缺词条时的原中文兜底（绝不输出空串）。
const (
	textFallbackLabel = "面包屑导航"
	textFallbackHome  = "首页"
)

// BuildView 生成面包屑渲染视图：作者手填层级优先，未填时按构建期页面访问路径派生。
//
// ctx 可为 nil（独立编译 / 单测）：此时派生出空层级，组件不渲染（而不是猜一个路径）。
func BuildView(p *Props, ctx *core.RenderContext) View {
	sep := strings.TrimSpace(p.Separator)
	if sep == "" {
		sep = defaultSeparator
	}
	v := View{
		Separator:       sep,
		Label:           textFallbackLabel,
		homeIndex:       -1,
		homeFromDefault: strings.TrimSpace(p.HomeLabel) == "",
		jsonLDEnabled:   p.JSONLD,
	}
	items := p.Items
	derived := len(items) == 0
	if derived {
		path := ""
		if ctx != nil {
			path = ctx.CurrentPath
		}
		items = deriveItems(path, homeOf(p))
		if len(items) > 0 {
			v.homeIndex = 0
		}
	}
	if p.HideHome && len(items) > 0 {
		items = items[1:]
		// 首页被隐藏后首项不再是首页：缺省首页名不再参与回填。
		v.homeIndex = -1
	}
	// 派生模式下只剩首页一项 = 当前页就是首页：没有层级可展示，整块不渲染。
	// 作者手填层级不受此限（那是他明确要求的展示）。
	if derived && !p.HideHome && len(items) < 2 {
		items = nil
		v.homeIndex = -1
	}
	// 站内链接本地化（审计 I18N-015）：只对**作者手填**的层级项 URL 生效 ——
	// 它们的语义是站内逻辑路径（/about），要按当前语言加前缀；派生模式的 URL 来自
	// ctx.CurrentPath（已是当前语言的访问路径），再过一次前缀会变成 /en/en/about。
	siteLink := (func(string) string)(nil)
	if !derived {
		siteLink = ctx.ResolveSiteLink
	}
	v.Items = itemViews(items, sep, siteLink)
	v.Visible = len(v.Items) > 0
	return v
}

// ApplyI18n 按当前语言填充容器无障碍标签与首页项文案（实现 core.I18nAware）。
//
// text 为 nil 或未命中词条时使用包内中文兜底，保证属性与首项永不为空。
// 结构化数据在文案定稿之后生成：与可见项共用同一份 Items，两处不会分叉。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.Label = textFallbackLabel
	} else {
		v.Label = text(TextKeyLabel, textFallbackLabel)
	}
	if v.homeFromDefault && v.homeIndex >= 0 && v.homeIndex < len(v.Items) {
		home := textFallbackHome
		if text != nil {
			if t := text(TextKeyHome, textFallbackHome); t != "" {
				home = t
			}
		}
		v.Items[v.homeIndex].Label = home
	}
	v.JSONLD = ""
	if v.jsonLDEnabled {
		v.JSONLD = jsonLDScript(v.Items)
	}
}

// homeOf 首页项文案：作者填的优先，缺省为包内中文兜底（构建期由 ApplyI18n 取词覆盖）。
func homeOf(p *Props) string {
	if h := strings.TrimSpace(p.HomeLabel); h != "" {
		return h
	}
	return textFallbackHome
}

// deriveItems 按构建期页面访问路径展开层级（首页 + 各路径段）。
//
// 与 head 的 JSON-LD 面包屑（seo_head.breadcrumbList）逐段同规则：首页固定第一项、
// 路径按 / 切分、段名取解码后的原文、链接取累积路径。规则刻意在两处各写一份 ——
// 组件包不能反向依赖 builder（会形成 import 环），等价性由
// internal/builder/breadcrumb_contract_test.go 的同源对比测试钉住。
//
// 路径为空（预览块 / 独立编译）时返回 nil：宁可不渲染，也不猜一个层级。
func deriveItems(rawPath, homeLabel string) []Item {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return nil
	}
	u, err := url.Parse(rawPath)
	if err != nil || u.Opaque != "" {
		return nil
	}
	items := []Item{{Label: homeLabel, URL: "/"}}
	cur := ""
	for _, seg := range strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/") {
		if seg == "" {
			continue
		}
		cur += "/" + seg
		name, uerr := url.PathUnescape(seg) // EscapedPath 已保证转义合法。
		if uerr != nil {
			name = seg
		}
		items = append(items, Item{Label: name, URL: cur})
	}
	return items
}

// itemViews 转换层级项并标记末项：末项即当前页（输出 is-current / aria-current）。
//
// siteLink 站内链接本地化器（审计 I18N-015，可空）：只对作者手填的 URL 传入，
// 派生模式传 nil（那些 URL 已经是当前语言的访问路径）。
func itemViews(items []Item, sep string, siteLink func(string) string) []ItemView {
	out := make([]ItemView, 0, len(items))
	for i, it := range items {
		raw := strings.TrimSpace(it.URL)
		if raw != "" && siteLink != nil {
			raw = siteLink(raw)
		} else if raw != "" {
			// 派生模式（按当前路径自动生成层级）拿到的是**已本地化的站内路径**，
			// 不走 siteLink 是对的（再本地化一次会把 /en/blog 变成 /en/en/blog），
			// 但仍然要补站点基址 —— 漏掉这一支的表现是「面包屑的首页与中间层级仍是
			// 根相对路径」，而同一页的导航已经绝对了，属于最难发现的那类不一致。
			raw = seo.AbsoluteSiteURL(raw)
		}
		iv := ItemView{
			Label:   strings.TrimSpace(it.Label),
			URL:     raw,
			Current: i == len(items)-1,
		}
		if i < len(items)-1 {
			iv.Separator = sep
		}
		out = append(out, iv)
	}
	return out
}

// ldListItem / ldDoc BreadcrumbList 结构化数据形状。
//
// 字段与 seo_head.buildJSONLD 里的面包屑逐项一致（@type / position / name / item），
// 保证同一页里两条来源的数据形状相同，不会让消费方看到两种写法。
type ldListItem struct {
	Type     string `json:"@type"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Item     string `json:"item,omitempty"`
}

type ldDoc struct {
	Context         string       `json:"@context"`
	Type            string       `json:"@type"`
	ItemListElement []ldListItem `json:"itemListElement"`
}

// jsonLDScript 由可见项生成 BreadcrumbList 脚本（同一份 Items，序号从 1 起）。
//
// json.Marshal 默认转义 < > &，作者填的 </script> 之类不会逃逸出脚本标签。
func jsonLDScript(items []ItemView) string {
	if len(items) == 0 {
		return ""
	}
	doc := ldDoc{Context: "https://schema.org", Type: "BreadcrumbList"}
	for i, it := range items {
		doc.ItemListElement = append(doc.ItemListElement, ldListItem{
			Type:     "ListItem",
			Position: i + 1,
			Name:     it.Label,
			Item:     it.URL,
		})
	}
	b, err := json.Marshal(doc)
	if err != nil {
		// 结构体只含字符串/整数，序列化不会失败；真失败时宁可不输出脚本，也不输出半截 JSON。
		return ""
	}
	return "<script type=\"application/ld+json\">" + string(b) + "</script>"
}
