package i18n

import (
	"sort"
	"strings"
)

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

func LangLabel(lang string) string {
	lang = strings.TrimSpace(lang)
	if v, ok := langLabels[lang]; ok {
		return v
	}
	return lang
}
