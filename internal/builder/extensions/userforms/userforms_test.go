package userforms

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

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

func TestEffectiveTitle(t *testing.T) {
	if got := effectiveTitle(&Props{Mode: ModeRegister}); got != formSpecs[ModeRegister].title {
		t.Fatalf("注册形态的默认标题不对: %q", got)
	}
	if got := effectiveTitle(&Props{Mode: ModeRegister, Title: "加入我们"}); got != "加入我们" {
		t.Fatalf("作者自定义标题应原样使用: %q", got)
	}
}

func TestCompileCSSIncludesFragmentStyles(t *testing.T) {
	var b core.CSSBuckets
	CompileCSS("t", &Props{Color: "#c00"}, &b)
	css := b.String()

	for _, want := range []string{
		".sky-user-form", ".sky-user-input", ".sky-user-submit", "#c00",
		".sky-user-meta", ".sky-user-grid", ".sky-user-device", ".sky-user-tag",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
}
