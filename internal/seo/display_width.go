package seo

import "unicode"

func DisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r <= 0x007F {
			w++
			continue
		}
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			w += 2
			continue
		}
		if r > 0x00FF {
			w += 2
			continue
		}
		w++
	}
	return w
}

func TruncateDisplayWidth(s string, max int) string {
	if max <= 0 || DisplayWidth(s) <= max {
		return s
	}
	const ellipsis = "…"
	budget := max
	if budget > 1 {
		budget--
	}
	out := make([]rune, 0, len([]rune(s)))
	for _, r := range s {
		cost := 1
		if r > 0x007F {
			if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) || r > 0x00FF {
				cost = 2
			}
		}
		if budget < cost {
			break
		}
		budget -= cost
		out = append(out, r)
	}
	return string(out) + ellipsis
}
