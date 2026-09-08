// Package templates — Jet 模板全局函数注入。
//
// 三个 Set（后台/工作台/组件）统一注入这些函数，模板里直接调用。
// 保持纯函数、无副作用，便于确定性构建与测试。
package templates

import (
	"strconv"
	"strings"

	"github.com/CloudyKit/jet/v6"
)

// injectGlobals 给 Jet Set 注入全局函数（所有 Set 共用同一套）。
func injectGlobals(set *jet.Set) {
	// 当前无全局函数需要注入；保留入口供后续扩展（多个 Set 共用）。
}

// thousandsUint uint64 千分位（无符号，避免 int64 溢出）。
func thousandsUint(n uint64) string {
	return thousandsFromStr(strconv.FormatUint(n, 10))
}

// thousands 整数千分位。
func thousands(n int64) string {
	return thousandsFromStr(strconv.FormatInt(n, 10))
}

// thousandsFromStr 对已格式化的数字串加千分位分隔。
func thousandsFromStr(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
