package templates

import (
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	seoscore "go_wp/internal/seo"

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
	set.AddGlobal("currencyPrefix", currencyPrefix)
	set.AddGlobal("percentChange", percentChange)
	set.AddGlobal("percentChangeDir", percentChangeDir)
	set.AddGlobal("sprintf", sprintf)
	set.AddGlobal("queryURL", queryURL)
	set.AddGlobal("pageBase", pageBase)
	set.AddGlobal("formatTime", formatTime)
	set.AddGlobal("truncateWidth", truncateWidth)
	set.AddGlobal("pageNumbers", pageNumbers)
	set.AddGlobal("pager", PagerOf)
	set.AddGlobal("amount", amount)
	set.AddGlobal("amountDash", amountDash)
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
	return seoscore.TruncateDisplayWidth(stringify(raw), width)
}

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
