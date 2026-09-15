package carticon

// carticon_test.go — 购物车图标组件的视图测试。
//
// 钉住的是**不变量**：
//   · 三种浮层用 <details>（无 JS 也能开合），只有 hover 形态不用 —— 桌面靠 CSS 悬停，
//     触屏靠 sr-only checkbox + 覆盖层 label 的「点击展开」（见 carticon.css 的 @hovernone）；
//   · 片段地址必须带工程 id（不带就永远拉不到购物车），且语言只在非空时带上；
//   · 槽位没配时图标退化为不可点（HasCartURL=false），模板据此不输出死链；
//   · 缺工程 id 时给可见提示，而不是渲染一个点开永远空着的图标。

import (
	"strings"
	"testing"
)

func baseProps() *Props { return &Props{} }

// TestEffectiveMode 形态解析：未知值兜底下拉浮层。
func TestEffectiveMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ModeDropdown},
		{"weird", ModeDropdown},
		{ModeDropdown, ModeDropdown},
		{ModeDrawer, ModeDrawer},
		{ModeModal, ModeModal},
		{ModeHover, ModeHover},
	}
	for _, tt := range tests {
		if got := effectiveMode(&Props{Mode: tt.in}); got != tt.want {
			t.Errorf("effectiveMode(%q)=%q, want=%q", tt.in, got, tt.want)
		}
	}
}

// TestBuildViewUsesDetailsExceptHover 浮层外壳：三种形态用 details，hover 不用。
func TestBuildViewUsesDetailsExceptHover(t *testing.T) {
	for _, mode := range []string{ModeDropdown, ModeDrawer, ModeModal} {
		v := BuildView(&Props{Mode: mode}, "proj-1", "zh-CN", "/cart")
		if !v.UseDetails {
			t.Errorf("形态 %s 应使用 <details>（原生可展开、键盘可达、无 JS 也能开）", mode)
		}
	}
	if v := BuildView(&Props{Mode: ModeHover}, "proj-1", "zh-CN", "/cart"); v.UseDetails {
		t.Error("hover 形态不该用 details：桌面靠 CSS 悬停，触屏靠 checkbox 点击展开")
	}
}

// TestBuildViewURLsCarrayProjectAndLang 片段地址必须带工程 id 与语言。
func TestBuildViewURLsCarrayProjectAndLang(t *testing.T) {
	v := BuildView(baseProps(), "proj-9", "en-US", "/cart")
	if !strings.Contains(v.CartViewURL, "projectId=proj-9") {
		t.Fatalf("购物车片段地址缺少工程 id: %q", v.CartViewURL)
	}
	if !strings.Contains(v.CartViewURL, "lang=en-US") {
		t.Fatalf("片段地址缺少语言: %q", v.CartViewURL)
	}
	if !strings.Contains(v.SummaryURL, "projectId=proj-9") {
		t.Fatalf("角标片段地址缺少工程 id: %q", v.SummaryURL)
	}
	if !strings.HasPrefix(v.CartViewURL, cartViewPath) || !strings.HasPrefix(v.SummaryURL, cartSummaryPath) {
		t.Fatalf("片段路径不对: %q / %q", v.CartViewURL, v.SummaryURL)
	}
}

// TestBuildViewOmitsEmptyLang 语言为空时不带 lang 参数（单语言站点不做无用判断）。
func TestBuildViewOmitsEmptyLang(t *testing.T) {
	v := BuildView(baseProps(), "proj-9", "  ", "/cart")
	if strings.Contains(v.CartViewURL, "lang=") {
		t.Fatalf("语言为空时不该带 lang 参数: %q", v.CartViewURL)
	}
}

// TestBuildViewWithoutProjectRendersNotice 缺工程 id 时给可见提示，不让整页构建失败。
//
// 与 addToCart 同一条边界：pipeline.DefaultCompile 明确不带工程 id，
// 把它当致命错误会让「一个图标坏了整页发布不了」。
func TestBuildViewWithoutProjectRendersNotice(t *testing.T) {
	for _, pid := range []string{"", "   "} {
		v := BuildView(baseProps(), pid, "zh-CN", "/cart")
		if v.Notice == "" {
			t.Fatal("缺工程 id 时应给可见提示，而不是渲染一个点开永远空着的图标")
		}
		if v.CartViewURL != "" {
			t.Fatalf("不可用时不该输出片段地址: %q", v.CartViewURL)
		}
	}
}

// TestBuildViewCartURLFallback 槽位没配时图标不可点（不输出死链）。
func TestBuildViewCartURLFallback(t *testing.T) {
	v := BuildView(baseProps(), "proj-1", "zh-CN", "")
	if v.HasCartURL {
		t.Fatal("槽位没配时不该有购物车页链接")
	}
	v2 := BuildView(baseProps(), "proj-1", "zh-CN", "  /cart  ")
	if !v2.HasCartURL || v2.CartURL != "/cart" {
		t.Fatalf("槽位路径应去空白后使用，实际 %q (has=%v)", v2.CartURL, v2.HasCartURL)
	}
}

// TestBuildViewIconFallback 图标名未知时回退默认图标（不做成空图标）。
func TestBuildViewIconFallback(t *testing.T) {
	v := BuildView(&Props{Icon: "not-a-real-icon-name"}, "proj-1", "zh-CN", "/cart")
	if strings.TrimSpace(v.IconSVG) == "" {
		t.Fatal("未知图标名应回退默认购物车图标，而不是输出空")
	}
}

// TestApplyI18n 界面文案：默认标签取译文，作者自定义的文案不动。
func TestApplyI18n(t *testing.T) {
	v := &View{Label: textFallbackLabel}
	v.ApplyI18n(func(key, fallback string) string {
		if key != TextKeyLabel {
			t.Errorf("文案键不对: %q", key)
		}
		return "Cart"
	})
	if v.Label != "Cart" {
		t.Fatalf("默认标签应取译文，实际 %q", v.Label)
	}

	v2 := &View{Label: "我的车"}
	v2.ApplyI18n(func(string, string) string { return "Cart" })
	if v2.Label != "我的车" {
		t.Fatalf("作者自定义的文案不该被界面翻译覆盖，实际 %q", v2.Label)
	}

	var v3 *View
	v3.ApplyI18n(nil) // 不 panic
}

// TestEffectiveLabel 标签文字缺省。
func TestEffectiveLabel(t *testing.T) {
	if got := effectiveLabel(&Props{}); got != defaultLabel {
		t.Fatalf("空标签应回退默认值，实际 %q", got)
	}
	if got := effectiveLabel(&Props{Label: "  "}); got != defaultLabel {
		t.Fatalf("纯空白应回退默认值，实际 %q", got)
	}
	if got := effectiveLabel(&Props{Label: "购物袋"}); got != "购物袋" {
		t.Fatalf("自定义标签应原样使用，实际 %q", got)
	}
}
