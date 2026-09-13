package userforms

// userforms_test.go — 访客账号表单组件的视图测试。
//
// 钉住的是**不变量**：
//   · 每个形态都要解析到正确的片段能力与内置页面（两张表放一起就是为了不漏改其一）；
//   · 片段地址必须带工程 id（不带就永远拉不到表单）；
//   · 缺工程 id 时给可见提示，而不是渲染一个永远空着的容器。

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

// TestBuildViewMapsEveryMode 每个形态都映射到自己的片段与内置页面。
func TestBuildViewMapsEveryMode(t *testing.T) {
	for mode, spec := range formSpecs {
		v := BuildView(&Props{Mode: mode}, "proj-9", "zh-CN")
		if !strings.Contains(v.FragmentURL, "/_fragments/"+spec.fragment+"?") {
			t.Errorf("形态 %s 的片段地址不对: %q", mode, v.FragmentURL)
		}
		if v.FallbackURL != spec.page {
			t.Errorf("形态 %s 的降级落点应为 %q，实际 %q", mode, spec.page, v.FallbackURL)
		}
		if v.FallbackText == "" {
			t.Errorf("形态 %s 缺少降级链接文案（无 JS 时容器不能是空的）", mode)
		}
	}
}

// TestBuildViewCarriesProjectLangAndNext 片段地址的三个要素。
func TestBuildViewCarriesProjectLangAndNext(t *testing.T) {
	v := BuildView(&Props{Mode: ModeLogin, Next: "/account"}, "proj-9", "en-US")
	for _, want := range []string{"projectId=proj-9", "lang=en-US", "next=%2Faccount"} {
		if !strings.Contains(v.FragmentURL, want) {
			t.Fatalf("片段地址缺少 %q: %q", want, v.FragmentURL)
		}
	}
}

// TestBuildViewOmitsEmptyNext 没配回跳时不带 next 参数（带了空值会让片段多做一次判断）。
func TestBuildViewOmitsEmptyNext(t *testing.T) {
	v := BuildView(&Props{Mode: ModeLogin}, "proj-9", "zh-CN")
	if strings.Contains(v.FragmentURL, "next=") {
		t.Fatalf("没配回跳时不该带 next 参数: %q", v.FragmentURL)
	}
}

// TestBuildViewWithoutProjectRendersNotice 缺工程 id 时给可见提示，不让整页构建失败。
func TestBuildViewWithoutProjectRendersNotice(t *testing.T) {
	for _, pid := range []string{"", "   "} {
		v := BuildView(&Props{Mode: ModeLogin}, pid, "zh-CN")
		if v.Notice == "" {
			t.Fatal("缺工程 id 时应给可见提示")
		}
		if v.FragmentURL != "" {
			t.Fatalf("不可用时不该输出片段地址: %q", v.FragmentURL)
		}
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

// TestCompileCSSIncludesFragmentStyles 组件必须把片段内容的基础样式一起带上。
//
// 片段 HTML 是运行时渲染的，页面作者在编辑器里看不到它 ——
// 不带样式的结果是「登录表单是一列裸输入框」。
func TestCompileCSSIncludesFragmentStyles(t *testing.T) {
	var b core.CSSBuckets
	CompileCSS("t", &Props{Color: "#c00"}, &b)
	css := b.String()
	// 账号中心四块用到的新类也要在（片段 HTML 运行时才渲染，样式必须由组件带上）。
	for _, want := range []string{
		".sky-user-form", ".sky-user-input", ".sky-user-submit", "#c00",
		".sky-user-meta", ".sky-user-grid", ".sky-user-device", ".sky-user-tag",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
}
