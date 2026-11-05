package jet

import (
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"


	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
)

func injectGlobals(set *jet.Set) {
	set.AddGlobal("money", money)
	set.AddGlobal("thousands", thousands)
	set.AddGlobal("percent", percent)
	set.AddGlobal("dateTime", dateTime)
	set.AddGlobal("date", date)
	set.AddGlobal("dateTimeLocal", dateTimeLocal)
	set.AddGlobal("siteDateTime", siteDateTime)
	set.AddGlobal("dateTimeSecond", dateTimeSecond)
	set.AddGlobal("fill", fill)
	set.AddGlobal("formValue", formValue)
	set.AddGlobal("formChecked", formChecked)
	set.AddGlobal("joinNonEmpty", joinNonEmpty)
	set.AddGlobal("has", has)
	set.AddGlobal("currencyPrefix", currencyPrefix)
	set.AddGlobal("percentChange", percentChange)
	set.AddGlobal("percentChangeDir", percentChangeDir)
	set.AddGlobal("sprintf", sprintf)
	set.AddGlobal("queryURL", queryURL)
	set.AddGlobal("pageBase", pageBase)
	set.AddGlobal("formatTime", formatTime)
	set.AddGlobal("truncateWidth", truncateWidth)
	set.AddGlobal("icon", iconGlobal())
	set.AddGlobal("pageNumbers", pageNumbers)
	set.AddGlobal("pager", PagerOf)
	set.AddGlobal("amount", amount)
	set.AddGlobal("amountDash", amountDash)
	set.AddGlobal("tokens", tokens)
	set.AddGlobal("tone", tone)
	set.AddGlobal("latency", latency)
	set.AddGlobal("navActive", NavActive)
	set.AddGlobal("navOpen", NavOpen)
	set.AddGlobal("navBranchActive", NavBranchActive)
	set.AddGlobal("navOwner", NavOwner)
	set.AddGlobal("navIsOwner", NavIsOwner)
	set.AddGlobal("navFirstURL", NavFirstURL)
}

func money(cents any, symbol string) string {
	n := toInt64("money", cents)

	if strings.TrimSpace(symbol) == "" {
		symbol = ""
	}

	whole, frac := n/100, n%100
	if frac < 0 {
		frac = -frac
	}
	body := strconv.FormatInt(whole, 10)

	if n < 0 && whole == 0 {
		body = "-0"
	}
	return symbol + body + "." + fmt.Sprintf("%02d", frac)
}

func thousands(n any) string {
	return groupThousands(strconv.FormatInt(toInt64("thousands", n), 10))
}

func percent(v any, digits int) string {
	f := toFloat64("percent", v)
	if digits < 0 {
		digits = 0
	}
	if digits > 6 {
		digits = 6
	}
	return strconv.FormatFloat(f, 'f', digits, 64) + "%"
}

// tone 把系列序号映射到配色槽位（CSS 里的 --chart-c1..--chart-c8，1 起）。
//
// 配色是显示格式，归模板：service 只交序列数组，选哪一档颜色不关它的事。
// 槽位数（8）是样式表的约定，写在模板层，改主题时只改这里和 CSS。
func tone(i any) int {
	n := toInt64("tone", i)
	// toInt64 交的是 int64（模板里的序号/计数都是它），这里转成 int 再取模：
	// CSS 槽位是 1..8 的小整数，返回 int 才对得上 --chart-cN 的写法。
	return int(n%8) + 1
}

// tokens 把 token 数按量级缩写成 1.2M / 34.5K 这样的短串。
//
// 这是显示格式，归模板：service 只交整数 token，换单位/换语言/换精度都不该改后端。
func tokens(v any) string {
	n := toInt64("tokens", v)
	switch {
	case n >= 1_000_000_000:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 1, 64) + "B"
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 10_000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

// latency 把毫秒数写成调用耗时（毫秒/秒两档）。
//
// 同样是显示格式：service 交 row.LatencyMs，怎么显示由模板决定。
func latency(v any) string {
	ms := toInt64("latency", v)
	if ms < 0 {
		ms = 0
	}
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + " ms"
	}
	return strconv.FormatFloat(float64(ms)/1000, 'f', 2, 64) + " s"
}

func dateTime(v any) string {
	t, ok := toTime("dateTime", v)
	if !ok {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

func date(v any) string {
	t, ok := toTime("date", v)
	if !ok {
		return ""
	}
	return t.Local().Format("2006-01-02")
}

func dateTimeLocal(v any) string {
	t, ok := toTime("dateTimeLocal", v)
	if !ok {
		return ""
	}
	local := t.Local()
	if local.Second() != 0 {
		return local.Format("2006-01-02T15:04:05")
	}
	return local.Format("2006-01-02T15:04")
}

func siteDateTime(v any) string {
	t, ok := toTime("siteDateTime", v)
	if !ok {
		return ""
	}
	return t.In(sitetz.Location()).Format("2006-01-02 15:04")
}

func dateTimeSecond(v any) string {
	t, ok := toTime("dateTimeSecond", v)
	if !ok {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func fill(pattern string, kv ...any) string {
	params := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		name, ok := kv[i].(string)
		if !ok {
			panic(misuse("fill", kv[i], "占位符名（字符串）"))
		}
		params[name] = stringify(kv[i+1])
	}
	out, ok := i18n.FillNamedPlaceholders(pattern, params)
	if !ok {
		logger.Scene("template").
			With("pattern", pattern).
			Warn("模板文案填充后仍有残留占位符（词条被改坏或参数名写错）")
	}
	return out
}

func formValue(echo url.Values, name string, fallback any) string {
	if echo != nil {
		if v, ok := echo[name]; ok && len(v) > 0 {
			return v[0]
		}
	}
	return stringify(fallback)
}

func formChecked(echo url.Values, name string, fallback bool) bool {
	if echo != nil {
		if v, ok := echo[name]; ok {
			if len(v) == 0 {
				return false
			}
			switch strings.ToLower(strings.TrimSpace(v[0])) {
			case "", "0", "false", "off", "no":
				return false
			default:
				return true
			}
		}
	}
	return fallback
}

// Stringify 把任意值转成模板输出的字符串（nil 为空串）。
func Stringify(v any) string { return stringify(v) }

func stringify(v any) string {
	if isNil(v) {
		return ""
	}
	return fmt.Sprint(v)
}

func DateTimeLocal(v any) string { return dateTimeLocal(v) }

func percentChange(v any, digits int) string {
	if isNil(v) {
		return ""
	}
	f := toFloat64("percentChange", v)
	if f > 0 {
		return "+" + percent(f, digits)
	}
	return percent(f, digits)
}

func percentChangeDir(v any) string {
	if isNil(v) {
		return "none"
	}
	switch f := toFloat64("percentChangeDir", v); {
	case f > 0:
		return "up"
	case f < 0:
		return "down"
	default:
		return "flat"
	}
}

// IsNil 是模板辅助函数的导出名，供适配层与测试使用。
func IsNil(v any) bool { return isNil(v) }

func isNil(v any) bool {
	if v == nil {
		return true
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

func joinNonEmpty(sep string, parts ...any) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(stringify(p)); v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, sep)
}

// has 判断一个集合里有没有某个值。
//
// 模板用它在「多值字段」上做显示判断，例如模型的 InputTypes 里有没有 "text"：
// {{if has(row.InputTypes, "text")}}checked{{end}}。
// 支持切片/数组（按元素比较）与 map（按键存在），其它类型一律 false ——
// 模板里拿到 nil 或标量时不该 panic，也不该悄悄为真。
func has(collection any, want any) bool {
	if collection == nil {
		return false
	}
	wantStr := stringify(want)
	rv := reflect.ValueOf(collection)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if stringify(rv.Index(i).Interface()) == wantStr {
				return true
			}
		}
		return false
	case reflect.Map:
		for _, k := range rv.MapKeys() {
			if stringify(k.Interface()) == wantStr {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func currencyPrefix(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "", "CNY", "RMB":
		return "¥"
	default:
		return strings.ToUpper(strings.TrimSpace(code)) + " "
	}
}

func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/3 + 1)
	if neg {
		b.WriteByte('-')
	}
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func misuse(fn string, v any, want string) error {
	return fmt.Errorf("模板函数 %s：期望%s，实际 %T（值 %v）", fn, want, v, v)
}

func toInt64(fn string, v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case uint64:
		if n > math.MaxInt64 {
			panic(misuse(fn, v, "int64 范围内的整数"))
		}
		return int64(n)
	case uint:
		if uint64(n) > math.MaxInt64 {
			panic(misuse(fn, v, "int64 范围内的整数"))
		}
		return int64(n)
	case *int64:
		if n == nil {
			return 0
		}
		return *n
	case float64:

		if n != math.Trunc(n) {
			panic(misuse(fn, v, "整数（money 收整数分；小数通常是把元当分传了）"))
		}
		if n >= maxInt64AsFloat || n < minInt64AsFloat {
			panic(misuse(fn, v, "int64 范围内的整数"))
		}
		return int64(n)
	case float32:
		return toInt64(fn, float64(n))
	case nil:
		return 0
	default:
		panic(misuse(fn, v, "整数"))
	}
}

func amount(v any) string {
	if !amountPresent(v) {
		return ""
	}
	return strconv.FormatFloat(toFloat64("amount", v), 'f', -1, 64)
}

func amountDash(v any) string {
	if !amountPresent(v) {
		return "—"
	}
	return strconv.FormatFloat(toFloat64("amountDash", v), 'f', -1, 64)
}

func amountPresent(v any) bool {
	switch n := v.(type) {
	case nil:
		return false
	case *float64:
		return n != nil
	}
	return true
}

func toFloat64(fn string, v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case *float64:
		if n == nil {
			return 0
		}
		return *n
	case nil:
		return 0
	default:

		return float64(toInt64(fn, v))
	}
}

func toTime(fn string, v any) (t time.Time, ok bool) {
	switch tv := v.(type) {
	case time.Time:
		if tv.IsZero() {
			return time.Time{}, false
		}
		return tv, true
	case *time.Time:
		if tv == nil || tv.IsZero() {
			return time.Time{}, false
		}
		return *tv, true
	case nil:
		return time.Time{}, false

	case utils.JSONTime:
		if tv.IsZero() {
			return time.Time{}, false
		}
		return tv.Time(), true
	case *utils.JSONTime:
		if tv == nil || tv.IsZero() {
			return time.Time{}, false
		}
		return tv.Time(), true
	default:
		panic(misuse(fn, v, "时间"))
	}
}

const (
	maxInt64AsFloat = 9223372036854775808.0
	minInt64AsFloat = -9223372036854775808.0
)

func stringifyAll(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = stringify(a)
	}
	return out
}

func sprintf(pattern string, args ...any) string {
	out := fmt.Sprintf(pattern, stringifyAll(args)...)
	if strings.Contains(out, "%!") || strings.Contains(out, "%s") {
		logger.Scene("template").
			With("pattern", pattern).
			Warn("模板位置填充后仍有未替换的占位符（词条被改坏或参数个数写错）")
	}
	return out
}

func queryURL(base string, kv ...any) string {
	return pagination.QueryURL(base, kv...)
}

func pageBase(raw string, drop ...string) string {
	return pagination.PageBase(raw, drop...)
}

func truncateWidth(raw any, width int) string {
	return utils.TruncateDisplayWidth(stringify(raw), width)
}

// FormatTime 是模板 formatTime 的导出名，供适配层使用。
func FormatTime(raw any) string { return formatTime(raw) }

func formatTime(raw any) string {
	switch v := raw.(type) {
	case time.Time:
		if v.IsZero() {
			return ""
		}
		return v.Local().Format("2006-01-02 15:04")
	case *time.Time:
		if v == nil || v.IsZero() {
			return ""
		}
		return v.Local().Format("2006-01-02 15:04")
	}
	s := strings.TrimSpace(stringify(raw))
	if s == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Local().Format("2006-01-02 15:04")
		}
	}
	return s
}

func pageNumbers(page, pages, win int) []int {
	if win != pagination.WindowSize {

		return legacyWindowNumbers(page, pages, win)
	}
	win2 := pagination.Window(page, pages)
	out := make([]int, 0, len(win2))
	for _, it := range win2 {
		if !it.Gap {
			out = append(out, it.Page)
		}
	}
	return out
}

func legacyWindowNumbers(page, pages, win int) []int {
	if pages <= 1 {
		return nil
	}
	if win < 3 {
		win = 3
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	if pages <= win {
		out := make([]int, 0, pages)
		for p := 1; p <= pages; p++ {
			out = append(out, p)
		}
		return out
	}
	half := (win - 3) / 2
	lo := page - half
	hi := page + half
	if lo < 2 {
		lo, hi = 2, lo+win-3
	}
	if hi > pages-1 {
		hi, lo = pages-1, pages-1-(win-3)
	}
	out := make([]int, 0, win)
	out = append(out, 1)
	for p := lo; p <= hi; p++ {
		if p < 2 || p > pages-1 {
			continue
		}
		out = append(out, p)
	}
	out = append(out, pages)
	return out
}
