package seo

import "unicode"

// DisplayWidth 估算 SERP 展示宽度（ASCII 1 单位，CJK/全角 2 单位）。
// 不测量真实字体，只用于标题/描述长度评分与预览截断。
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

// TruncateDisplayWidth 按展示宽度截断，append 省略号。
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
