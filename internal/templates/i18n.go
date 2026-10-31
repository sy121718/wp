package templates

import (
	"strings"

	"go_wp/pkg/i18n"
)

func TranslateFunc(lang string) func(key, fallback string) string {
	return i18n.TranslateFunc(lang)
}

type LanguageOption struct {
	Code   string
	Label  string
	Active bool
}

func LanguageOptions(current string) []LanguageOption {
	current = strings.TrimSpace(current)
	langs := i18n.AvailableLangs()
	options := make([]LanguageOption, 0, len(langs))
	matched := false
	for _, code := range langs {
		active := code == current
		matched = matched || active
		options = append(options, LanguageOption{
			Code: code, Label: i18n.LangLabel(code), Active: active,
		})
	}
	if !matched && len(options) > 0 {
		options[0].Active = true
	}
	return options
}
