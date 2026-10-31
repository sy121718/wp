package templates

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
)

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

		{`{{ joinNonEmpty("-", "A", 12) }}`, "A-12"},

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

func TestFormChecked(t *testing.T) {
	vars := jet.VarMap{}
	vars.Set("echo", url.Values{"sameBilling": {"1"}, "empty": {""}, "zero": {"0"}})
	var nilEcho url.Values
	vars.Set("nilEcho", nilEcho)

	cases := []struct {
		expr   string
		expect string
	}{

		{`{{if formChecked(echo, "sameBilling", false)}}checked{{else}}no{{end}}`, "checked"},

		{`{{if formChecked(echo, "absent", true)}}checked{{else}}no{{end}}`, "checked"},
		{`{{if formChecked(echo, "absent", false)}}checked{{else}}no{{end}}`, "no"},

		{`{{if formChecked(echo, "empty", true)}}checked{{else}}no{{end}}`, "no"},
		{`{{if formChecked(echo, "zero", true)}}checked{{else}}no{{end}}`, "no"},

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
		{"siteDateTime", `{{ siteDateTime(zero) }}`, ""},
		{"dateTimeSecond", `{{ dateTimeSecond(zero) }}`, ""},
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

func TestUnregisteredGlobalFails(t *testing.T) {
	if _, err := renderGlobal(t, `{{ noSuchFunc(1) }}`, nil); err == nil {
		t.Fatal("未注册的函数不应可调用")
	}
}

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

func TestMoneyRejectsNonNumber(t *testing.T) {
	if _, err := renderGlobal(t, `{{ money("abc", "¥") }}`, nil); err == nil {
		t.Fatal("字符串入参应当渲染失败")
	}
}

func TestMoneyRejectsFractionalCents(t *testing.T) {
	if _, err := renderGlobal(t, `{{ money(12.5, "¥") }}`, nil); err == nil {
		t.Fatal("小数分应当渲染失败（元/分传错）")
	}
}

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

func TestDateTimeRejectsNonTime(t *testing.T) {
	if _, err := renderGlobal(t, `{{ dateTime("2026-10-07") }}`, nil); err == nil {
		t.Fatal("字符串入参应当渲染失败")
	}
}

const (
	sitetzEnvKey   = "GO_WP_SITE_TIMEZONE"
	sitetzShanghai = "Asia/Shanghai"
)

func useShanghaiSiteAndUTCLocal(t *testing.T) {
	t.Helper()
	oldLocal := time.Local
	time.Local = time.UTC
	t.Setenv(sitetzEnvKey, sitetzShanghai)
	sitetz.ResetForTest()
	t.Cleanup(func() {
		sitetz.ResetForTest()
		time.Local = oldLocal
	})
	if got := sitetz.Name(); got != sitetzShanghai {
		t.Fatalf("站点时区未生效（得到 %q，期望 %q）：时区库不可用或解析失败，本组判据失去区分力", got, sitetzShanghai)
	}
}

func TestSiteDateTimeUsesSiteTimezone(t *testing.T) {
	useShanghaiSiteAndUTCLocal(t)

	at := time.Date(2026, 10, 7, 1, 5, 0, 0, time.UTC)
	jt := utils.NewJSONTime(at)
	var nilPtr *time.Time
	var nilJTPtr *utils.JSONTime
	vars := jet.VarMap{}
	vars.Set("at", at)
	vars.Set("jt", jt)
	vars.Set("jtPtr", &jt)
	vars.Set("nilPtr", nilPtr)
	vars.Set("nilJTPtr", nilJTPtr)
	vars.Set("zero", time.Time{})

	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ siteDateTime(at) }}`, "2026-10-07 09:05"},
		{`{{ siteDateTime(jt) }}`, "2026-10-07 09:05"},
		{`{{ siteDateTime(jtPtr) }}`, "2026-10-07 09:05"},
		{`{{ siteDateTime(zero) }}`, ""},
		{`{{ siteDateTime(nilPtr) }}`, ""},
		{`{{ siteDateTime(nilJTPtr) }}`, ""},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}

	if got := mustRender(t, `{{ dateTime(at) }}`, vars); got != "2026-10-07 01:05" {
		t.Fatalf("对照口径 dateTime 期望 %q，实际 %q（本地时区被改动？本组判据失去区分力）", "2026-10-07 01:05", got)
	}

	if _, err := renderGlobal(t, `{{ dateTimeIn("Asia/Shanghai", at) }}`, vars); err == nil {
		t.Fatal("模板里不应存在按字面量指定时区的入口")
	}
}

func TestDateTimeSecondKeepsSeconds(t *testing.T) {
	useShanghaiSiteAndUTCLocal(t)

	at := time.Date(2026, 10, 7, 9, 5, 7, 0, time.UTC)
	jt := utils.NewJSONTime(at)
	var nilPtr *time.Time
	var nilJTPtr *utils.JSONTime
	vars := jet.VarMap{}
	vars.Set("at", at)
	vars.Set("jt", jt)
	vars.Set("jtPtr", &jt)
	vars.Set("nilPtr", nilPtr)
	vars.Set("nilJTPtr", nilJTPtr)
	vars.Set("zero", time.Time{})

	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ dateTimeSecond(at) }}`, "2026-10-07 09:05:07"},
		{`{{ dateTimeSecond(jt) }}`, "2026-10-07 09:05:07"},
		{`{{ dateTimeSecond(jtPtr) }}`, "2026-10-07 09:05:07"},
		{`{{ dateTimeSecond(zero) }}`, ""},
		{`{{ dateTimeSecond(nilPtr) }}`, ""},
		{`{{ dateTimeSecond(nilJTPtr) }}`, ""},

		{`{{ dateTime(at) }}`, "2026-10-07 09:05"},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			if got := mustRender(t, c.expr, vars); got != c.expect {
				t.Fatalf("期望 %q，实际 %q", c.expect, got)
			}
		})
	}
}

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

func TestFill(t *testing.T) {
	cases := []struct {
		expr   string
		expect string
	}{
		{`{{ fill("减 {value}%", "value", 20) }}`, "减 20%"},
		{`{{ fill("减 {value} 元", "value", "20.00") }}`, "减 20.00 元"},
		{`{{ fill("共 {count} 条，列出最近 {limit} 条。", "count", 37, "limit", 50) }}`, "共 37 条，列出最近 50 条。"},
		{`{{ fill("没有占位符", "value", 1) }}`, "没有占位符"},

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

func TestFillLeavesUnknownPlaceholderVisible(t *testing.T) {
	got := mustRender(t, `{{ fill("减 {value}%", "valeu", 20) }}`, nil)
	if got != "减 {value}%" {
		t.Fatalf("残留占位符应原样露出，实际 %q", got)
	}
}

func TestFillRejectsNonStringName(t *testing.T) {
	if _, err := renderGlobal(t, `{{ fill("减 {value}%", 1, 20) }}`, nil); err == nil {
		t.Fatal("非字符串占位符名应当渲染失败")
	}
}

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
