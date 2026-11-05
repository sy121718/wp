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

// StringPlaceholderCount 是「这句话能不能被纯字符串参数填满」的唯一判定。
//
// 返回 ok=false 时，调用方一律退归口文案，绝不交给 fmt.Sprintf —— 因为下面任何一条
// 不成立都会在屏幕上留下 %!s(MISSING) / %!(EXTRA ...) / %!d(int=3) 这类标记：
//
//   - 所有动词必须是 %s（%d / %v 等不接受字符串参数）；
//   - 不允许显式索引 %[1]s：参数少于索引上限时 fmt 直接吐标记；
//   - 不允许宽度/精度/星号（%05s / %.2s / %*s）：宽度会改写输出与参数消费规则；
//   - %%%% 是字面百分号，既不算占位符，也不让整句作废。
//
// 返回的 count 就是可填位置数：调用方用它核对参数个数，多一个少一个都归口。
// 这里刻意只有这一个扫描器 —— 另起一个「只数 %s」的计数器必然会与它判定口径漂移。
func StringPlaceholderCount(s string) (count int, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++
			continue
		}
		// 裸 %s 只有这一种合法形态：% 后面紧跟 s。
		// 中间出现任何修饰（索引 [n]、标志 +-# 0、宽度、精度 .、星号 *）都不接受。
		if i+1 >= len(s) || s[i+1] != 's' {
			return 0, false
		}
		count++
		i++
	}
	return count, true
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
