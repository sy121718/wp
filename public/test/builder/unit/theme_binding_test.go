package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

const themeDoc = `{"settings":{"layout":{"mode":"full"}},"root":[
  {"id":"btn1","type":"core.button","props":{"action":"external","text":"按钮","value":"https://example.com/x"}},
  {"id":"form1","type":"core.form","props":{"submitLabel":"提交","fields":[{"type":"text","label":"姓名","name":"name"}]}}
]}`

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

	for _, want := range []string{
		"--sky-c-primary: #111111",
		"--sky-btn-bg: #111111",
		"--sky-btn-hover-bg: #333333",
	} {
		if !strings.Contains(compiled.ThemeVarsCSS, want) {
			t.Errorf("主题变量没有进产物：缺少 %q", want)
		}
	}

	if !strings.Contains(compiled.CSS, "var(--sky-btn-bg, var(--sky-c-primary, #2563eb))") {
		t.Errorf("提交按钮应经 var(--sky-btn-bg) 消费主题色")
	}
}

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
