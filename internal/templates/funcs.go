// Package templates — Jet 模板全局函数注入。
//
// 这里放「模板自己就能算、但每个页面各写一遍必然分叉」的纯函数：金额、时间、百分比、
// 千分位。判据是**无请求上下文**：不读 gin.Context、不查库、不看当前语言。
//
// 为什么语言相关的取词（t）不在这里：语言是**请求级**的（URL 前缀 / cookie /
// Accept-Language 每请求解析一次），而全局函数是进程启动期注册一次 —— 一个进程级函数
// 拿不到「当前请求的语言」。所以 t 仍由 shell.Prepare 注入渲染数据。
//
// —— 两条 Jet v6.3.2 的实测行为，决定了本文件的形状（别按直觉改回去）——
//
//  1. **模板里的数字字面量一律是 float64**：`{{ typeOf(1000) }}` 输出 `float64`。
//     所以入参声明成具体整数类型会让 `{{ thousands(1000) }}` 静默失败；本文件的入参
//     收敛必须接受 float64。
//
//  2. **函数返回的 error 会被丢弃**：`evalPipeCallExpression` 只取 `returns[0]`
//     （`if len(returns) == 0 { … }; return returns[0], nil`），第二返回值不参与判定。
//     实测 `func(x any) (string, error)` 在传错类型时渲染出**空串且不报错** ——
//     那正是最坏的失败模式（金额位空着，没人发现）。所以本文件返回纯值，
//     传错类型直接 **panic**：`exec.go` 的 `defer st.recover(&err)` 会把 error 类型的
//     panic 转成模板错误 → 页面 500 / 构建失败，错误当场暴露。
package templates

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
	"go_wp/pkg/utils"
)

// injectGlobals 给 Jet Set 注入全局函数（所有 Set 共用同一套）。
//
// 五个函数覆盖后台与组件模板里反复出现的四类格式化。新增一个之前先问：
// 「这是不是纯函数、且两个以上的模板会用到」—— 只服务一个页面的格式化留在视图层，
// 不要往这里堆。
func injectGlobals(set *jet.Set) {
	set.AddGlobal("money", money)
	set.AddGlobal("thousands", thousands)
	set.AddGlobal("percent", percent)
	set.AddGlobal("dateTime", dateTime)
	set.AddGlobal("date", date)
	set.AddGlobal("dateTimeLocal", dateTimeLocal)
	set.AddGlobal("fill", fill)
	set.AddGlobal("formValue", formValue)
	set.AddGlobal("formChecked", formChecked)
	set.AddGlobal("joinNonEmpty", joinNonEmpty)
	set.AddGlobal("currencyPrefix", currencyPrefix)
	set.AddGlobal("percentChange", percentChange)
	set.AddGlobal("percentChangeDir", percentChangeDir)
}

// money 金额（**整数分**）→ 展示串：符号 + 两位小数，如 ¥1234.50。
//
// 口径四条（每条都对应一个实测过的坑）：
//   - **不做 /100 之外的换算**，不引入浮点：分 → 元只在这里发生一次，全站一条口径；
//   - 负数走整数除法与取余分别取值，**不做 -cents** —— math.MinInt64 取反会溢出成负数，
//     产出「--92233720368547758.08」这种串（旧实现 orderAmountText 有这个问题）；
//   - symbol **原样前置**：只把「全空白」当成没符号，其余一律按调用点给的串拼接 ——
//     ¥ 这类符号不需要空格，三字母代码（USD）需要，那一个空格由调用点写在 symbol 里
//     （币种符号的真源是 sys_dict 的 symbol 列，不在代码里另建映射）；
//   - **小数一律拒绝**：`money(12.5, "¥")` 是把「元」当「分」传，属于编程错误，
//     当场 panic 而不是打印 ¥12.50（那个数字看起来完全合理）。
//
// 不做千分位分组：现有页面（order 的 orderMoneyLabel / orderAmountText）都不分组，
// 这里保持一致 —— 分组与否是**显示口径**，要改就一次全站改，不要两套并存。
func money(cents any, symbol string) string {
	n := toInt64("money", cents)
	// 全空白当成「没有币种符号」；有内容就原样前置（空格由调用点决定）。
	if strings.TrimSpace(symbol) == "" {
		symbol = ""
	}

	whole, frac := n/100, n%100
	if frac < 0 {
		frac = -frac
	}
	body := strconv.FormatInt(whole, 10)
	// whole == 0 时 FormatInt 给的是 "0"，丢掉负号：-5 分要显示成 -0.05 而不是 0.05。
	if n < 0 && whole == 0 {
		body = "-0"
	}
	return symbol + body + "." + fmt.Sprintf("%02d", frac)
}

// thousands 整数千分位：1234567 → 1,234,567。
func thousands(n any) string {
	return groupThousands(strconv.FormatInt(toInt64("thousands", n), 10))
}

// percent 数值 → 百分比串，digits 为小数位（0..6，越界收敛）。
//
// **不带正负号**：`+12.5%` / `-3.0%` 里的符号是显示决定（涨跌配色也挂在它上面），
// 由调用点自己拼 —— 工具函数替调用点决定符号，就会出现「有的页面有 +、有的没有」。
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

// dateTime 时间 → 本地时区、分钟精度（2006-01-02 15:04）。
//
// 零值 / nil / 空指针一律返回空串，由模板决定显示什么（`{{if .PaidAt}}`）——
// 工具函数不该替页面选「—」还是「未支付」。
func dateTime(v any) string {
	t, ok := toTime("dateTime", v)
	if !ok {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// date 时间 → 本地时区日期（2006-01-02）。
func date(v any) string {
	t, ok := toTime("date", v)
	if !ok {
		return ""
	}
	return t.Local().Format("2006-01-02")
}

// dateTimeLocal 时间 → `<input type="datetime-local">` 的 value（2006-01-02T15:04）。
//
// 三条与控件行为对齐的规则（不写清楚就会被当成「格式怪」改回去）：
//   - **必须带 T**：空格形态不在 HTML 规范里（Chrome 实测会顺手规范化，那是实现宽容，
//     不能依赖）；纯日期形态会被控件直接清成空串，回填就丢了；
//   - **秒非 0 时输出到秒**：模板里那个输入框带 step="1"，秒被截断时控件按 stepMismatch
//     判非法 —— 症状是「表单根本提交不了」，而不是显示不对；
//   - 亚秒一律丢弃：datetime-local 无法安全表达微秒，而业务时间精确到秒已经过头。
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

// fill 用命名参数填充文案里的 `{name}` 占位符：`fill(tr("k", "减 {value}%"), "value", 20)`。
//
// 为什么需要它：词条里的占位符是 `{value}` 这种命名形态（不是 %s —— 词条正文里就有裸 `%`，
// 「减 20%」用 Sprintf 会输出乱码），而模板侧原先没法填，于是每个带数字的文案都在 Go 里
// 用 i18n.FillTranslate 拼好再传下来。有了它，文案与数字一起留在模板里。
//
// 参数成对（名, 值），值为任意类型（fmt.Sprint 收敛）。**填完仍有残留占位符时记警告并
// 原样返回**：那意味着词条被改坏或参数名写错，页面上会露出 `{value}` 字面量 ——
// 比「补 0 渲染出一句看起来像成功结论的错话」安全（同 i18n.FillNamedPlaceholders 的取舍）。
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

// formValue 表单回填取值：**本次提交**里有这个字段就用提交值，否则用初始值。
//
// 判据是「字段在不在提交里」（不是「值是不是空」）：用户刻意清空的字段必须回显成空，
// 若按空值判断就会把数据库里的旧值填回去 —— 那是「删不掉的内容」，比丢输入更难发现。
//
// echo 为 nil（首屏渲染、没有提交）时一律用初始值。
func formValue(echo url.Values, name string, fallback any) string {
	if echo != nil {
		if v, ok := echo[name]; ok && len(v) > 0 {
			return v[0]
		}
	}
	return stringify(fallback)
}

// formChecked 复选框的回填状态：**本次提交里有这个字段**就用提交值（勾了 = 有值），
// 没有才用初始值。
//
// 与 formValue 的区别在「缺键」的语义：文本框缺键意味着「没提交这个字段」→ 用初始值；
// 而复选框**不勾选就是不提交**，此时必须回落到初始值（而不是当成 false）——
// 否则「失败重渲后我勾的选项没了」，而用户会以为是自己没勾。
//
// 值判定按 HTML 的写法收敛：空串 / "0" / "false" 都算没勾（浏览器与手写表单两种形态都覆盖）。
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

// stringify 把任意值收敛成展示串（nil → 空串，其余走 fmt.Sprint）。
//
// **typed nil 也要当空串**：dto 里的可空字段是 *time.Time / *uint64 / *int64，
// 直接 fmt.Sprint 会打出 "<nil>" —— 页面上就是一行「<nil>」而不是空着。
// （Jet 传不进裸 nil 字面量，所以真实场景里进来的永远是 typed nil，这条是必须的。）
func stringify(v any) string {
	if isNil(v) {
		return ""
	}
	return fmt.Sprint(v)
}

// DateTimeLocal 是 dateTimeLocal 的**跨包导出**：服务端偶尔也要产出同一种写法
// （例：批量启停要把库里已有的时间窗原样回送给「整体更新」接口）。两处各写一份格式化
// 必然漂移，而漂移的表现是「批量操作把时间窗改成了别的时刻」—— 静默改数据。
func DateTimeLocal(v any) string { return dateTimeLocal(v) }

// percentChange 变化率 → **带符号**的百分比串（`+12.5%` / `-3.0%` / `0.0%`；nil → 空串）。
//
// 与 percent 分开：percent 不带符号（符号是显示决定），而变化率这个场景里
// 「涨还是跌」必须一眼看出来，符号本身就是信息。nil（无基期）返回空串 ——
// 显示 `+0.0%` 会让「上期一单没卖」看起来像「持平」。
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

// percentChangeDir 变化率的方向：`none`（无基期）/ `up` / `down` / `flat`（恰好 0）。
//
// 单独给一个方向而不复用 percentChange 的文本：模板要按方向挂不同的类（涨/跌配色），
// 而从 `"+12.5%"` 里切首字符判断方向在 Jet 里做不到（没有字符串索引）。
// **恰好 0 是 flat 而不是 down**：0.0% 画一个向下的箭头是错的。
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

// isNil 判断「没有值」：裸 nil 与 typed nil（*float64 等）都算。
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

// joinNonEmpty 逐段丢掉空值后用 sep 拼接：`joinNonEmpty(" ", country, province, city)`。
//
// 为什么需要它：地址这类「多段可选」的展示，各段常常只填一半（省填了、区没填），
// 直接按固定顺序输出会得到「上海  浦东」这种双空格，或者留一个空段看不出是「没填」
// 还是「丢了」。判据与 Go 侧的 orderAddressLabel 逐字一致（丢掉空段、空格连接）。
func joinNonEmpty(sep string, parts ...any) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(stringify(p)); v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, sep)
}

// currencyPrefix 币种代码 → 金额前缀（人民币符号 ¥，其它币种用代码 + 空格）。
//
// 与既有 orderMoneyLabel 的规则逐字一致（空 / CNY / RMB → ¥，其余 → 大写代码 + 空格），
// 目的是让页面从 dto 的 currency 字段自己拼前缀，而不是让 Go 预先拼好整串金额。
//
// 币种符号的真源本应是 sys_dict 的 symbol 列（AGENTS.md §数据库），这里是**过渡**：
// 改成读字典要连「取不到时降级」一起设计（符号缺失只该少个前缀，不该把金额打空），
// 不在本次重构范围。
func currencyPrefix(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "", "CNY", "RMB":
		return "¥"
	default:
		return strings.ToUpper(strings.TrimSpace(code)) + " "
	}
}

// groupThousands 给已格式化的十进制整数串加千分位（保留前导负号）。
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

// —— 入参收敛 ——
//
// 模板里的字段可能是 int64 / int / *int64 / float64（dto 里元与分混用是既有事实），
// 加上「字面量一律 float64」这条 Jet 行为，所以入参声明成 any 再收敛。
// **认不出来一律 panic**，不静默取零值、不打印一个看起来合理的数字。

// misuse 组装「传错类型」的 panic 值：消息里带函数名与实际类型，便于从 500 的日志里定位。
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
		// 字面量走这里（Jet 把数字字面量统一成 float64）。小数是编程错误：
		// money 收的是「分」，12.5 意味着调用点把「元」传了进来。
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
		// 整数也算合法（调用点常把计数当百分比传）。
		return float64(toInt64(fn, v))
	}
}

// toTime 收敛时间入参：ok=false 表示「没有时刻」（nil / 零值），调用方给空串。
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
	// dto 的可空时间字段是 *utils.JSONTime（跨模块传递的不可变形状），模板里直接写
	// `{{dateTime(.Coupon.StartsAt)}}` 是最自然的写法 —— 工具函数认它，调用点不必
	// 记得先转成 *time.Time（那种「记得转一下」的约定迟早有人忘，而忘了就是 panic）。
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

// int64 边界的浮点形态（2^63 与 -2^63 都是 2 的幂，可被 float64 精确表示）。
const (
	maxInt64AsFloat = 9223372036854775808.0
	minInt64AsFloat = -9223372036854775808.0
)
