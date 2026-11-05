package publish

import "strings"

// dirAttr 返回 HTML dir 属性：RTL 语言为 "rtl"，其余为空（由浏览器按 default 处理）。
//
// 发布内核只需要语言方向这一个纯事实，不依赖构建器。
func dirAttr(lang string) string {
	if isRTLLocale(lang) {
		return "rtl"
	}
	return ""
}

var rtlBaseSubtags = map[string]struct{}{
	"ar": {}, "arc": {}, "ckb": {}, "dv": {}, "fa": {}, "he": {}, "khw": {},
	"ks": {}, "ku": {}, "ps": {}, "sd": {}, "ug": {}, "ur": {}, "yi": {},
}

func isRTLLocale(lang string) bool {
	base := strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(base, "-_"); i >= 0 {
		base = base[:i]
	}
	if base == "" {
		return false
	}
	_, ok := rtlBaseSubtags[base]
	return ok
}
