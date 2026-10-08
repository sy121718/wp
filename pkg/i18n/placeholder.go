package i18n

import (
	"regexp"
	"strings"
)

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

// —— 命名占位符（`{name}`）：新词条的占位符约定 ——
//
// 为什么新词条不用 `%s` + fmt.Sprintf（两条静默故障）：
//
//  1. 词条会被运营在后台词条管理页里改。一旦改出裸 `%`（「折扣 50%」），Sprintf 会把它
//     当格式化动词，页面上出现 `%!s(MISSING)` 之类的乱码 —— 库中现存词条里就有裸 `%`；
//  2. 中英词条的占位符数量或顺序不一致时，Sprintf 会静默错配（把「删除 3 个」填成
//     「3 个都未删除」那种），位置参数对此毫无防护。
//
// 命名占位符把这两条都变成可判定的：填完之后还剩 `{xxx}` 就是**没填干净**，
// 调用方丢弃该条、落兜底文案 —— 绝不把 `{count}` 这样的字面量显示给运营看。
//
// 存量 `%s` / `%d` 词条由各自的批次迁移，本文件不改变它们的读法。

// namedPlaceholderPattern 命名占位符：`{count}` / `{max}` / `{name}`。
//
// 只认「字母数字下划线」—— 不含 `%`、空格与中文，避免把正文里的普通花括号
// （如 JSON 片段 `{"a":1}`）误当成占位符。
var namedPlaceholderPattern = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

// HasNamedPlaceholders 判断文本里是否还有命名占位符（未填充 / 词条写错时用）。
func HasNamedPlaceholders(text string) bool {
	return namedPlaceholderPattern.MatchString(text)
}

// FillNamedPlaceholders 用命名参数填充词条里的 `{name}` 占位符。
//
// 返回 (填充后的文本, ok)。ok=false 表示**填完仍有残留**（参数名拼错、词条被改坏），
// 调用方应当丢弃这一条并落兜底文案 —— 缺参数时不补默认值：补 0 会渲染出
// 「已删除 0 个」这种看起来像成功结论的错话，比不显示更危险。
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

// ZeroNamedPlaceholders 把全部 `{name}` 占位符替换成字面量 "0"。
//
// 用途是**受控回执的读侧归一**（internal/shell 的 NoticeTemplate 只认 %s/%d）：
// 写侧把真实计数填进 {count} 得到的句子，与「占位符全填 0」的候选模板做数字归一后
// 逐字相等，才说明整句出自本仓库 —— 手拼的 query 参数不可能命中。
func ZeroNamedPlaceholders(tpl string) string {
	return namedPlaceholderPattern.ReplaceAllString(tpl, "0")
}

// FillTranslate 取词（tr 是 TranslateFunc 的产物）后用命名参数填充 `{name}` 占位符。
//
// 词条被改坏（填完仍有残留占位符）时回落 fallback 原文再填一次 —— 页面上宁可显示
// 代码里那句写死的中文，也不能把 `{count}` 这样的字面量摆给运营看。
func FillTranslate(tr func(key, fallback string) string, key, fallback string, params map[string]string) string {
	if text, ok := FillNamedPlaceholders(tr(key, fallback), params); ok {
		return text
	}
	text, _ := FillNamedPlaceholders(fallback, params)
	return text
}
