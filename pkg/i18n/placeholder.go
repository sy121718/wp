package i18n

import "strings"

// 格式化占位符检测（Go fmt 动词）。
// 与 response 的 key|param 协议、AI 翻译占位符校验共用，见 i18n-issues 2-6。

// fmtVerbs 全部 Go 格式化动词。
const fmtVerbs = "vTtbcdoqxXUeEfFgGsp"

// skipFormatTail 从 % 后一位开始跳过 [n] 索引、flags、width、precision，返回动词下标。
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

// CountPlaceholders 统计字符串中的格式化占位符数量（%% 转义不计，%[n]s 计 1）。
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

// HasStringPlaceholdersOnly 判断字符串只包含字符串占位符（%s / %[n]s / %%）。
// key|param 协议只允许这类占位符：参数按字符串注入，%d/%f 等会输出 Go 格式错误文本。
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
