package languages

import (
	"strings"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type ItemView struct {
	Lang string

	Label string

	Href string

	Current bool
}

type View struct {
	Items []ItemView

	HasLinks bool

	Label string
}

const (
	TextKeyLabel = "site.component.languages.label"
)

const textFallbackLabel = "语言"

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
				continue
			}
		}
		items = append(items, ItemView{Lang: lang, Label: label, Href: href, Current: l.Current})
	}
	v.Items = items
	v.HasLinks = len(items) >= 2
	return v
}

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

func Endonym(lang string) string {
	code := strings.TrimSpace(lang)
	if code == "" {
		return ""
	}
	if name, ok := endonyms[code]; ok {
		return name
	}

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
