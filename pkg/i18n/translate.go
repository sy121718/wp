package i18n

import "strings"

func Translate(key, fallback, lang string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		if fallback != "" {
			return fallback
		}
		return key
	}

	if text := GetText(key, strings.TrimSpace(lang)); text != "" && text != key {
		return text
	}
	if fallback != "" {
		return fallback
	}
	return key
}

func TranslateFunc(lang string) func(key, fallback string) string {
	lang = strings.TrimSpace(lang)
	return func(key, fallback string) string {
		return Translate(key, fallback, lang)
	}
}
