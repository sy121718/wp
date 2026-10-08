package templates

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/CloudyKit/jet/v6"
)

// funcs_test.go — 模板全局函数（funcs.go）的判据。
//
// 全部经**真实 Jet Set 渲染**断言，而不是直接调 Go 函数：注册漏了（injectGlobals 里少一行）、
// Jet 对某种入参/返回形状的处理与直觉不符，都只有在渲染路径上才会暴露 ——
// 本文件里「字面量是 float64」「error 返回值被丢弃」两条就是这么做出来的。
// 复用 raw_pipe_test.go 的建 Set 方式，保证测的是同一个注入点。

// renderGlobal 用注入过全局函数的 Set 渲染一小段模板。
func renderGlobal(t *testing.T, src string, vars jet.VarMap) (string, error) {
	t.Helper()
	loader := jet.NewInMemLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	injectGlobals(set)
	loader.Set("t.jet", src)
	tpl, err := set.GetTemplate("t.jet")
	if err != nil {
		t.Fatalf("取模板失败: %v", err)
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, vars, nil); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func mustRender(t *testing.T, src string, vars jet.VarMap) string {
	t.Helper()
	out, err := renderGlobal(t, src, vars)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	return out
}

// TestJoinNonEmpty 多段可选字段的拼接：空段丢掉，不留双空格、不留空段。
func TestJoinNonEmpty(t *testing.T) {
	var nilPtr *string
	vars := jet.VarMap{}
	vars.Set("nilPtr", nilPtr)
	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ joinNonEmpty(" ", "中国", "上海", "", "浦东") }}`, "中国 上海 浦东"},
		{`{{ joinNonEmpty(" ", "", "  ", "") }}`, ""},
		{`{{ joinNonEmpty(" ", "中国") }}`, "中国"},
		{`{{ joinNonEmpty(" · ", "顺丰", "SF123") }}`, "顺丰 · SF123"},
		// 数字段（订单号 / 门牌号）也要能拼。
		{`{{ joinNonEmpty("-", "A", 12) }}`, "A-12"},
		// typed nil（dto 的可空字段）必须当空段：fmt.Sprint 会打出 "<nil>"，
		// 页面上就是一行「<nil>」而不是空着。Jet 传不进裸 nil 字面量，所以真实场景
		// 进来的永远是 typed nil —— 这条是必须的。
		{`{{ joinNonEmpty(" ", nilPtr, "上海") }}`, "上海"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestCurrencyPrefix 币种代码 → 金额前缀（与既有 orderMoneyLabel 的规则逐字一致）。
func TestCurrencyPrefix(t *testing.T) {
	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ currencyPrefix("") }}`, "¥"},
		{`{{ currencyPrefix("CNY") }}`, "¥"},
		{`{{ currencyPrefix("cny") }}`, "¥"},
		{`{{ currencyPrefix("RMB") }}`, "¥"},
		{`{{ currencyPrefix("USD") }}`, "USD "},
		{`{{ currencyPrefix("usd") }}`, "USD "},
		{`{{ currencyPrefix(" EUR ") }}`, "EUR "},
		// 与 money 组合后是页面上真正显示的金额串。
		{`{{ money(123450, currencyPrefix("USD")) }}`, "USD 1234.50"},
		{`{{ money(123450, currencyPrefix("CNY")) }}`, "¥1234.50"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, nil); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestFormChecked 复选框回填：不勾选 = 不提交 = 缺键 → 回落初始值；
// 提交了空串 / "0" / "false" 都算没勾（浏览器与手写表单两种形态）。
func TestFormChecked(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("echo", url.Values{"sameBilling": {"1"}, "empty": {""}, "zero": {"0"}})
	var nilEcho url.Values
	vars.Set("nilEcho", nilEcho)

	cases := []struct {
		expr   string
		expect string
	}{
		// 勾了：提交值非空 → true（初始值无关）
		{`{{if formChecked(echo, "sameBilling", false)}}checked{{else}}no{{end}}`, "checked"},
		// 没勾：缺键 → 回落初始值（true 时保持勾选）
		{`{{if formChecked(echo, "absent", true)}}checked{{else}}no{{end}}`, "checked"},
		{`{{if formChecked(echo, "absent", false)}}checked{{else}}no{{end}}`, "no"},
		// 提交了但值为空 / "0" → 没勾（**不回落到初始值**：用户明确取消了这个勾选）
		{`{{if formChecked(echo, "empty", true)}}checked{{else}}no{{end}}`, "no"},
		{`{{if formChecked(echo, "zero", true)}}checked{{else}}no{{end}}`, "no"},
		// 首屏（没有提交）：一律用初始值
		{`{{if formChecked(nilEcho, "sameBilling", true)}}checked{{else}}no{{end}}`, "checked"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestGlobalsRegistered 全部全局函数都必须注册且可调用。
//
// 判据是「真的调一次并拿到值」，不是「名字存在」：只断言 err != nil 的话，
// 一个未注册的名字同样报错 —— 那样这条判据在删除注册后**照样是绿的**。
func TestGlobalsRegistered(t *testing.T) {
	cases := []struct {
		name   string
		expr   string
		expect string
	}{
		{"money", `{{ money(100, "¥") }}`, "¥1.00"},
		{"thousands", `{{ thousands(1000) }}`, "1,000"},
		{"percent", `{{ percent(1.5, 1) }}`, "1.5%"},
		{"dateTime", `{{ dateTime(zero) }}`, ""},
		{"date", `{{ date(zero) }}`, ""},
	}
	vars := jet.VarMap{}
	vars.Set("zero", time.Time{})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("全局函数 %s 不可用：期望 %q，实际 %q", c.name, c.expect, got)
			}
		})
	}
}

// TestUnregisteredGlobalFails 反面对照：没注册的名字调用必须失败
// （防上一条退化成「什么名字都能渲染」）。
func TestUnregisteredGlobalFails(t *testing.T) {
	if _, err := renderGlobal(t, `{{ noSuchFunc(1) }}`, nil); err == nil {
		t.Fatal("未注册的函数不应可调用")
	}
}

// TestNumericLiteralIsFloat64 钉住 Jet 的行为本身：模板里的数字字面量是 float64。
//
// 这条不是好奇 —— 它决定了入参收敛必须接受 float64；一旦 Jet 升级后改成 int64，
// 这个判据会红，提醒改 toInt64 而不是让页面静默空串。
func TestNumericLiteralIsFloat64(t *testing.T) {
	loader := jet.NewInMemLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	set.AddGlobal("typeOf", func(v any) string { return fmt.Sprintf("%T", v) })
	loader.Set("t.jet", `{{ typeOf(1000) }}`)
	tpl, err := set.GetTemplate("t.jet")
	if err != nil {
		t.Fatalf("取模板失败: %v", err)
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, nil, nil); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if got := sb.String(); got != "float64" {
		t.Fatalf("数字字面量类型变了（当前 %s）：toInt64 的 float64 分支需要跟着改", got)
	}
}

// TestMoney 金额格式化：分 → 符号 + 两位小数。
func TestMoney(t *testing.T) {
	cases := []struct {
		name   string
		expr   string
		expect string
	}{
		{"零", `{{ money(0, "¥") }}`, "¥0.00"},
		{"整数元", `{{ money(123450, "¥") }}`, "¥1234.50"},
		{"一位小数补零", `{{ money(5, "¥") }}`, "¥0.05"},
		{"负的不足一元", `{{ money(-5, "¥") }}`, "¥-0.05"},
		{"负数", `{{ money(-150, "¥") }}`, "¥-1.50"},
		{"无符号", `{{ money(123450, "") }}`, "1234.50"},
		{"符号带空格原样前置", `{{ money(100, "USD ") }}`, "USD 1.00"},
		{"全空白符号等于无符号", `{{ money(100, "  ") }}`, "1.00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mustRender(t, c.expr, nil); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestMoneyRejectsNonNumber 传错类型必须**当场报错**。
//
// Jet 会丢弃函数返回的 error（只取第一个返回值），所以「返回 (值, error)」这条路
// 实测是静默空串 —— 本函数必须靠 panic 才能红。判据因此断言 err != nil。
func TestMoneyRejectsNonNumber(t *testing.T) {
	if _, err := renderGlobal(t, `{{ money("abc", "¥") }}`, nil); err == nil {
		t.Fatal("字符串入参应当渲染失败")
	}
}

// TestMoneyRejectsFractionalCents 小数分是编程错误：把「元」当「分」传进来。
// 它打印出来的数字（¥12.50）看起来完全合理，所以必须拒绝而不是四舍五入。
func TestMoneyRejectsFractionalCents(t *testing.T) {
	if _, err := renderGlobal(t, `{{ money(12.5, "¥") }}`, nil); err == nil {
		t.Fatal("小数分应当渲染失败（元/分传错）")
	}
}

// TestMoneyMinInt64NotOverflow 单独钉住溢出这条：旧实现（-cents）会产出
// 「--92233720368547758.08」。用常量而不是字面量，避免抄错数字让判据空转。
func TestMoneyMinInt64NotOverflow(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("min", int64(math.MinInt64))
	got := mustRender(t, `{{ money(min, "") }}`, vars)
	if strings.HasPrefix(got, "--") {
		t.Fatalf("取反溢出：%q", got)
	}
	if got != "-92233720368547758.08" {
		t.Fatalf("期望 %q，实际 %q", "-92233720368547758.08", got)
	}
}

// TestThousands 千分位。
func TestThousands(t *testing.T) {
	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ thousands(0) }}`, "0"},
		{`{{ thousands(999) }}`, "999"},
		{`{{ thousands(1000) }}`, "1,000"},
		{`{{ thousands(1234567) }}`, "1,234,567"},
		{`{{ thousands(-999) }}`, "-999"},
		{`{{ thousands(-1000) }}`, "-1,000"},
		{`{{ thousands(-1234567) }}`, "-1,234,567"},
		{`{{ thousands(1000000) }}`, "1,000,000"},
		{`{{ thousands(-1000000) }}`, "-1,000,000"},
		{`{{ thousands(12345678901) }}`, "12,345,678,901"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, nil); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestPercent 百分比：不带符号（符号是调用点的显示决定），小数位收敛到 0..6。
func TestPercent(t *testing.T) {
	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ percent(12.5, 1) }}`, "12.5%"},
		{`{{ percent(-3.0, 1) }}`, "-3.0%"},
		{`{{ percent(33.333, 2) }}`, "33.33%"},
		{`{{ percent(12.6, 0) }}`, "13%"},
		{`{{ percent(12.6, -1) }}`, "13%"},
		{`{{ percent(12.6, 99) }}`, "12.600000%"},
		{`{{ percent(0, 1) }}`, "0.0%"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, nil); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestDateTimeAndDate 时间格式化：零值 / nil / 空指针给空串，由模板决定显示什么。
func TestDateTimeAndDate(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 5, 30, 0, time.Local)
	vars := jet.VarMap{}
	vars.Set("at", at)
	var nilPtr *time.Time
	vars.Set("nilPtr", nilPtr)
	vars.Set("zero", time.Time{})

	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ dateTime(at) }}`, "2026-10-07 09:05"},
		{`{{ date(at) }}`, "2026-10-07"},
		{`{{ dateTime(zero) }}`, ""},
		{`{{ date(zero) }}`, ""},
		{`{{ dateTime(nilPtr) }}`, ""},
		{`{{ date(nilPtr) }}`, ""},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestDateTimeRejectsNonTime 时间函数同样要拒绝错误类型（字符串不是时间）。
func TestDateTimeRejectsNonTime(t *testing.T) {
	if _, err := renderGlobal(t, `{{ dateTime("2026-10-07") }}`, nil); err == nil {
		t.Fatal("字符串入参应当渲染失败")
	}
}

// TestDateTimeLocal datetime-local 控件的 value 形态：带 T、秒非 0 才到秒。
//
// 三条规则各对应一个真实故障：不带 T 会被控件清空（回填丢失）、秒被截断时控件按
// stepMismatch 判非法（表单提交不了）、亚秒无法表达。
func TestDateTimeLocal(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("whole", time.Date(2026, 10, 7, 9, 5, 0, 0, time.Local))
	vars.Set("withSec", time.Date(2026, 10, 7, 9, 5, 30, 0, time.Local))
	vars.Set("withNano", time.Date(2026, 10, 7, 9, 5, 30, 123456789, time.Local))
	vars.Set("zero", time.Time{})
	var nilPtr *time.Time
	vars.Set("nilPtr", nilPtr)

	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ dateTimeLocal(whole) }}`, "2026-10-07T09:05"},
		{`{{ dateTimeLocal(withSec) }}`, "2026-10-07T09:05:30"},
		{`{{ dateTimeLocal(withNano) }}`, "2026-10-07T09:05:30"},
		{`{{ dateTimeLocal(zero) }}`, ""},
		{`{{ dateTimeLocal(nilPtr) }}`, ""},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestFill 命名占位符填充（词条里的 {value} 形态）。
func TestFill(t *testing.T) {
	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ fill("减 {value}%", "value", 20) }}`, "减 20%"},
		{`{{ fill("减 {value} 元", "value", "20.00") }}`, "减 20.00 元"},
		{`{{ fill("共 {count} 条，列出最近 {limit} 条。", "count", 37, "limit", 50) }}`, "共 37 条，列出最近 50 条。"},
		{`{{ fill("没有占位符", "value", 1) }}`, "没有占位符"},
		// 词条里的裸 % 不能被当格式化动词（这正是用命名占位符而不是 Sprintf 的理由）。
		{`{{ fill("减 {value}%", "value", 20) }}`, "减 20%"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, nil); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

// TestFillLeavesUnknownPlaceholderVisible 参数名写错 / 词条被改坏时，
// 残留占位符**原样露出**而不是被补成 0（补 0 会渲染出一句看起来像成功结论的错话）。
func TestFillLeavesUnknownPlaceholderVisible(t *testing.T) {
	got := mustRender(t, `{{ fill("减 {value}%", "valeu", 20) }}`, nil)
	if got != "减 {value}%" {
		t.Fatalf("残留占位符应原样露出，实际 %q", got)
	}
}

// TestFillRejectsNonStringName 参数名必须是字符串（位置写错属于编程错误）。
func TestFillRejectsNonStringName(t *testing.T) {
	if _, err := renderGlobal(t, `{{ fill("减 {value}%", 1, 20) }}`, nil); err == nil {
		t.Fatal("非字符串占位符名应当渲染失败")
	}
}

// TestFormValue 表单回填：**提交里有这个字段就用提交值**（哪怕提交的是空串），
// 提交里没有才用初始值。
//
// 「用户刻意清空的字段回显成空」这条是核心：按「值是不是空」判断会把库里的旧值填回去，
// 表现是「这个字段删不掉」，比丢输入更难发现。
func TestFormValue(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("echo", url.Values{
		"name":  {"用户刚打的字"},
		"empty": {""},
	})
	var nilEcho url.Values
	vars.Set("nilEcho", nilEcho)

	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ formValue(echo, "name", "库里的旧值") }}`, "用户刚打的字"},
		{`{{ formValue(echo, "empty", "库里的旧值") }}`, ""},
		{`{{ formValue(echo, "absent", "库里的旧值") }}`, "库里的旧值"},
		{`{{ formValue(nilEcho, "name", "库里的旧值") }}`, "库里的旧值"},
		{`{{ formValue(nilEcho, "name", "") }}`, ""},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}
