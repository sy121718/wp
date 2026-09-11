package unit

// theme_binding_test.go — 主题色是否真的能驱动组件产物。
//
// 起因：走查页上的「提交」按钮是蓝色（#2563eb），而项目里没有主题记录 —— 颜色其实来自
// 组件的内置 fallback（var(--sky-btn-bg, var(--sky-c-primary, #2563eb))）。
// 这组测试把「主题设置 → CSS 变量 → 组件消费」这条链路钉住：只要链路通，配了主题就一定变色。

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// themeDoc 一个含按钮与表单提交按钮的最小页面。
const themeDoc = `{"settings":{"layout":{"mode":"full"}},"root":[
  {"id":"btn1","type":"core.button","props":{"action":"external","text":"按钮","value":"https://example.com/x"}},
  {"id":"form1","type":"core.form","props":{"submitLabel":"提交","fields":[{"type":"text","label":"姓名","name":"name"}]}}
]}`

// TestThemeColorsReachComponents 主题的按钮色 / 主色会写进 :root 变量，组件按 var() 消费。
func TestThemeColorsReachComponents(t *testing.T) {
	page, err := builder.ParsePage([]byte(themeDoc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	theme := builder.ThemeSettings{}
	theme.Colors.Primary = "#111111"
	theme.Button.Background = "#111111"
	theme.Button.HoverBackground = "#333333"
	compiled, err := compile(t, page, builder.WithThemeSettings(&theme))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// 主题变量走 ThemeVarsCSS（document.jet 里在组件 CSS 之前单独输出），不在 compiled.CSS 里。
	for _, want := range []string{
		"--sky-c-primary: #111111",
		"--sky-btn-bg: #111111",
		"--sky-btn-hover-bg: #333333",
	} {
		if !strings.Contains(compiled.ThemeVarsCSS, want) {
			t.Errorf("主题变量没有进产物：缺少 %q", want)
		}
	}
	// 组件侧消费的是变量而不是写死的颜色：蓝色只作为变量缺失时的 fallback 出现。
	if !strings.Contains(compiled.CSS, "var(--sky-btn-bg, var(--sky-c-primary, #2563eb))") {
		t.Errorf("提交按钮应经 var(--sky-btn-bg) 消费主题色")
	}
}

// TestThemeAbsentFallsBackToBuiltin 没有主题时（themes 表为空）组件走内置默认色 ——
// 这不是「主题没生效」，而是根本没有主题可注入。
func TestThemeAbsentFallsBackToBuiltin(t *testing.T) {
	page, err := builder.ParsePage([]byte(themeDoc))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	compiled, err := compile(t, page)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(compiled.ThemeVarsCSS, "--sky-btn-bg:") {
		t.Errorf("没有主题时不该凭空输出 --sky-btn-bg")
	}
	if !strings.Contains(compiled.CSS, "#2563eb") {
		t.Errorf("没有主题时按钮应落到内置默认色（#2563eb）")
	}
}
