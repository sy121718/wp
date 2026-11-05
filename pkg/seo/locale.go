package seo

import "strings"

func isCJKLocale(locale string) bool {
	return locale == "" || strings.HasPrefix(strings.ToLower(locale), "zh")
}

func wordCount(text, locale string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	if isCJKLocale(locale) {
		return len([]rune(text))
	}
	return len(strings.Fields(text))
}
