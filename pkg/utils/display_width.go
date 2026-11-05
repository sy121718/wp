package utils

import "unicode"

// DisplayWidth 返回字符串在等宽显示下的宽度：ASCII 与拉丁扩展字符算 1，
// 汉字/假名/谚文与 BMP 以外的字符算 2。
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

// TruncateDisplayWidth 按显示宽度截断并补省略号。
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
