// languages — Jet 渲染路径辅助导出（与其余组件同形：Go 侧准备视图数据，模板只拼 HTML）。
package languages

import (
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出语言切换器样式编译。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// ItemView 单个语言条目的渲染视图。
type ItemView struct {
	// Lang 语言码（BCP-47 形态，如 zh-CN）：同时用于 hreflang 与 lang 属性。
	Lang string
	// Label 展示名（语言自称：简体中文 / English / 日本語 …）。
	Label string
	// Href 该语言的对应页面地址（当前语言为空——当前项渲染为不可点的 <span>）。
	Href string
	// Current 是否当前语言（aria-current 标记，不可点）。
	Current bool
}

// View 语言切换器渲染视图。
type View struct {
	Items []ItemView
	// HasLinks 是否渲染整个切换器：少于两种语言（或语言前缀未开启导致多语言
	// 映射到同一路径）时不渲染——单语言站点产物与 P3 之前逐字节一致。
	HasLinks bool
	// Label 容器 aria-label（构建期按当前语言取词，实现 core.I18nAware 回填）。
	Label string
}

// 访客面组件文案 key：site.component.{type}.{prop}（docs/06-D §10.3）。
const (
	// TextKeyLabel 切换器容器 aria-label 的词条 key。
	TextKeyLabel = "site.component.languages.label"
)

// textFallbackLabel 缺词条时的原中文兜底（绝不输出空串）。
const textFallbackLabel = "语言"

// ApplyI18n 按当前语言填充容器无障碍标签（实现 core.I18nAware）。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.Label = textFallbackLabel
		return
	}
	v.Label = text(TextKeyLabel, textFallbackLabel)
}

// BuildView 生成语言切换器视图：链接清单来自构建期注入的 ctx.Locales。
//
// 缺语言回退策略 S2「隐藏」（docs/06-D §9）：装配层已经只注入「本页存在独立路径」
// 的语言条目，这里再兜一层——不足两条即整块不渲染。理由见包注释与 docs 记录：
// 静态访问面没有运行时回退（/site 直接映射文件），指向不存在语言的链接必然 404，
// 而「指向默认语言回退页」会与产物 head 的 hreflang/canonical 互相矛盾。
func BuildView(_ *core.Node, p *Props, ctx *core.RenderContext) View {
	var v View
	if ctx == nil || len(ctx.Locales) == 0 {
		return v
	}
	items := make([]ItemView, 0, len(ctx.Locales))
	for _, l := range ctx.Locales {
		lang := strings.TrimSpace(l.Lang)
		if lang == "" {
			continue
		}
		label := Endonym(lang)
		if p != nil && p.ShowCode {
			label = label + " (" + lang + ")"
		}
		href := ""
		if !l.Current {
			href = strings.TrimSpace(l.Href)
			if href == "" {
				continue // 无地址可跳：隐藏该语言（S2），绝不输出空 href
			}
		}
		items = append(items, ItemView{Lang: lang, Label: label, Href: href, Current: l.Current})
	}
	v.Items = items
	v.HasLinks = len(items) >= 2
	return v
}

// endonyms 语言自称表（切换器惯例：用目标语言自己的写法，而不是当前界面语言的译名）。
// 未收录的语言回退语言码本身，绝不输出空串。
var endonyms = map[string]string{
	"zh": "中文", "zh-CN": "简体中文", "zh-Hans": "简体中文",
	"zh-TW": "繁體中文", "zh-HK": "繁體中文", "zh-Hant": "繁體中文",
	"en": "English", "en-US": "English", "en-GB": "English", "en-AU": "English",
	"ja": "日本語", "ja-JP": "日本語", "ko": "한국어", "ko-KR": "한국어",
	"fr": "Français", "fr-FR": "Français", "de": "Deutsch", "de-DE": "Deutsch",
	"es": "Español", "es-ES": "Español", "pt": "Português", "pt-BR": "Português",
	"it": "Italiano", "it-IT": "Italiano", "ru": "Русский", "ru-RU": "Русский",
	"nl": "Nederlands", "pl": "Polski", "tr": "Türkçe", "ar": "العربية",
	"vi": "Tiếng Việt", "th": "ไทย", "id": "Bahasa Indonesia", "ms": "Bahasa Melayu",
}

// Endonym 返回语言的自称：先精确匹配语言码，再退到主语言子标签，最后回退语言码。
func Endonym(lang string) string {
	code := strings.TrimSpace(lang)
	if code == "" {
		return ""
	}
	if name, ok := endonyms[code]; ok {
		return name
	}
	// 主语言子标签（en-US → en）大小写不敏感地尝试一次。
	if i := strings.IndexAny(code, "-_"); i > 0 {
		primary := code[:i]
		if name, ok := endonyms[primary]; ok {
			return name
		}
		for k, name := range endonyms {
			if strings.EqualFold(k, primary) {
				return name
			}
		}
	}
	for k, name := range endonyms {
		if strings.EqualFold(k, code) {
			return name
		}
	}
	return code
}
