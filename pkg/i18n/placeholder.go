package i18n

import (
	"regexp"
	"strings"
)

const fmtVerbs = "vTtbcdoqxXUeEfFgGsp"

func skipFormatTail(s string, j int) int {
	if j < len(s) && s[j] == '[' {
		for j < len(s) && s[j] != ']' {
			j++
		}
		if j < len(s) {
			j++
		}
	}
	for j < len(s) && strings.IndexByte("+- #0123456789.*", s[j]) >= 0 {
		j++
	}
	return j
}

func CountPlaceholders(s string) int {
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++
			continue
		}
		j := skipFormatTail(s, i+1)
		if j < len(s) && strings.IndexByte(fmtVerbs, s[j]) >= 0 {
			count++
			i = j
		}
	}
	return count
}

func HasStringPlaceholdersOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++
			continue
		}
		j := skipFormatTail(s, i+1)
		if j >= len(s) || s[j] != 's' {
			return false
		}
		i = j
	}
	return true
}

var namedPlaceholderPattern = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

func HasNamedPlaceholders(text string) bool {
	return namedPlaceholderPattern.MatchString(text)
}

func FillNamedPlaceholders(tpl string, params map[string]string) (string, bool) {
	out := tpl
	for name, value := range params {
		out = strings.ReplaceAll(out, "{"+name+"}", value)
	}
	if HasNamedPlaceholders(out) {
		return out, false
	}
	return out, true
}

func ZeroNamedPlaceholders(tpl string) string {
	return namedPlaceholderPattern.ReplaceAllString(tpl, "0")
}

func FillTranslate(tr func(key, fallback string) string, key, fallback string, params map[string]string) string {
	if text, ok := FillNamedPlaceholders(tr(key, fallback), params); ok {
		return text
	}
	text, _ := FillNamedPlaceholders(fallback, params)
	return text
}
