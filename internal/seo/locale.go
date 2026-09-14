package seo

import "strings"

// isCJKLocale 是否中文语境（影响字数/句长/段长统计口径，SEO-001）。
func isCJKLocale(locale string) bool {
	return locale == "" || strings.HasPrefix(strings.ToLower(locale), "zh")
}

// wordCount 按语言统计正文字数：CJK 按 rune，英文按空白分词。
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
