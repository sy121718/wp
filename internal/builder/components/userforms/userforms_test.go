package userforms

// userforms_test.go — 访客账号表单组件的视图测试。
//
// 钉住的是**不变量**：
//   · 所有形态复用同一个静态账号区块；
//   · 初值和文案不依赖运行时 HTML 或固定业务页面；
//   · 作者自定义标题与各模式基础样式保持一致。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestEffectiveMode 形态解析：未知值兜底登录。
func TestEffectiveMode(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"", ModeLogin},
		{"weird", ModeLogin},
		{ModeLogin, ModeLogin},
		{ModeRegister, ModeRegister},
		{ModeForgot, ModeForgot},
		{ModeReset, ModeReset},
		{ModeAccount, ModeAccount},
		{ModeProfile, ModeProfile},
		{ModePreference, ModePreference},
		{ModePassword, ModePassword},
		{ModeSessions, ModeSessions},
	} {
		if got := effectiveMode(&Props{Mode: tt.in}); got != tt.want {
			t.Errorf("effectiveMode(%q)=%q, want=%q", tt.in, got, tt.want)
		}
	}
}

// TestBuildViewIsStatic 所有模式脱离工程槽位，文案在构建期准备。
func TestBuildViewIsStatic(t *testing.T) {
	for mode := range formSpecs {
		v := BuildView(&Props{Mode: mode}, "", "en-US")
		if v.Mode != mode || v.Labels.Login.Identity == "" || len(v.Titles) != 9 {
			t.Fatalf("模式%s缺少静态表单配置: %+v", mode, v)
		}
		v.ApplyI18n(func(key, fallback string) string {
			if key == TextKeyTitleLogin {
				return "Sign in"
			}
			if key == "site.fragment.user.login.identity" {
				return "Email or username"
			}
			return fallback
		})
		if v.Labels.Login.Identity != "Email or username" {
			t.Fatalf("表单标签没有跟随构建语言")
		}
		if mode == ModeLogin && v.Title != "Sign in" {
			t.Fatalf("默认标题未翻译: %s", v.Title)
		}
	}
	v := BuildView(&Props{Title: "加入我们"}, "", "en-US")
	v.ApplyI18n(func(_, fallback string) string { return "translated" })
	if v.Title != "加入我们" {
		t.Fatalf("作者标题被覆盖: %s", v.Title)
	}
}

// TestEffectiveTitle 标题缺省取该形态的默认名。
func TestEffectiveTitle(t *testing.T) {
	if got := effectiveTitle(&Props{Mode: ModeRegister}); got != formSpecs[ModeRegister].title {
		t.Fatalf("注册形态的默认标题不对: %q", got)
	}
	if got := effectiveTitle(&Props{Mode: ModeRegister, Title: "加入我们"}); got != "加入我们" {
		t.Fatalf("作者自定义标题应原样使用: %q", got)
	}
}

// TestCompileCSSIncludesFragmentStyles 组件必须编译账号表单所有模式的基础样式。
func TestCompileCSSIncludesFragmentStyles(t *testing.T) {
	var b core.CSSBuckets
	CompileCSS("t", &Props{Color: "#c00"}, &b)
	css := b.String()
	// 账号资料与设备列表使用同一套局部样式。
	for _, want := range []string{
		".sky-user-form", ".sky-user-input", ".sky-user-submit", "#c00",
		".sky-user-meta", ".sky-user-grid", ".sky-user-device", ".sky-user-tag",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
}
