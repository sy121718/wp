package i18n

import (
	"sort"
	"strings"
)

// langLabels 后台语言下拉自称（不随界面语言变化）；未知语言码回退为码本身。
var langLabels = map[string]string{
	"zh-CN": "简体中文",
	"zh-TW": "繁體中文",
	"en-US": "English",
	"en-GB": "English (UK)",
	"ja":    "日本語",
	"ko":    "한국어",
	"fr":    "Français",
	"de":    "Deutsch",
	"es":    "Español",
	"pt-BR": "Português (BR)",
	"ru":    "Русский",
	"ar":    "العربية",
}

// AvailableLangs 返回 sys_i18n 中已有词条的语言集合（升序）。
// 缓存未加载时回退 zh-CN / en-US，保证后台语言切换可用。
func AvailableLangs() []string {
	cache.mu.RLock()
	seen := map[string]struct{}{}
	for _, langs := range cache.data {
		for lang := range langs {
			if lang = strings.TrimSpace(lang); lang != "" {
				seen[lang] = struct{}{}
			}
		}
	}
	cache.mu.RUnlock()

	if len(seen) == 0 {
		return []string{"zh-CN", "en-US"}
	}
	out := make([]string, 0, len(seen))
	for lang := range seen {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

// LangLabel 语言自称；未知码回退为语言码本身。
func LangLabel(lang string) string {
	lang = strings.TrimSpace(lang)
	if v, ok := langLabels[lang]; ok {
		return v
	}
	return lang
}
