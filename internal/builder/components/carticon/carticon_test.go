package carticon

// carticon_test.go — 购物车图标组件的视图测试。
//
// 钉住的是**不变量**：
//   · 三种浮层用 <details>（无 JS 也能开合），只有 hover 形态不用 —— 桌面靠 CSS 悬停，
//     触屏靠 sr-only checkbox + 覆盖层 label 的「点击展开」（见 carticon.css 的 @hovernone）；
//   · 片段地址必须带工程 id（不带就永远拉不到购物车），且语言只在非空时带上；
//   · 未配置结算动作时只展示购物车，不推断全站页面；
//   · 缺工程 id 时给可见提示，而不是渲染一个点开永远空着的图标。

import (
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
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
		v := BuildView(&Props{Mode: mode}, "proj-1", "zh-CN")
		if !v.UseDetails {
			t.Errorf("形态 %s 应使用 <details>（原生可展开、键盘可达、无 JS 也能开）", mode)
		}
	}
	if v := BuildView(&Props{Mode: ModeHover}, "proj-1", "zh-CN"); v.UseDetails {
		t.Error("hover 形态不该用 details：桌面靠 CSS 悬停，触屏靠 checkbox 点击展开")
	}
}

// TestBuildViewURLsCarrayProjectAndLang 片段地址必须带工程 id 与语言。
func TestBuildViewURLsCarrayProjectAndLang(t *testing.T) {
	v := BuildView(baseProps(), "proj-9", "en-US")
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
	v := BuildView(baseProps(), "proj-9", "  ")
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
		v := BuildView(baseProps(), pid, "zh-CN")
		if v.Notice == "" {
			t.Fatal("缺工程 id 时应给可见提示，而不是渲染一个点开永远空着的图标")
		}
		if v.CartViewURL != "" {
			t.Fatalf("不可用时不该输出片段地址: %q", v.CartViewURL)
		}
	}
}

// 未配置动作时保留纯购物车，不猜测任何全站页面。
func TestBuildViewDoesNotInferCheckoutTarget(t *testing.T) {
	v := BuildView(baseProps(), "proj-1", "zh-CN")
	if v.HasCheckoutAction || v.CheckoutAction.URL != "" {
		t.Fatal("不能推断结算目标")
	}
	inline := BuildView(&Props{Mode: ModeInline}, "proj-1", "zh-CN")
	if !inline.Inline || inline.UseDetails {
		t.Fatal("完整购物车应直接可见")
	}
}

func TestCheckoutActionConsumesOnlyResolvedTarget(t *testing.T) {
	node := &core.Node{ID: "cart-a", Actions: map[string]core.ActionTarget{"checkout": {Kind: core.ActionInstance, NodeID: "checkout-b", PartKey: "checkout"}}}
	v := BuildView(baseProps(), "project", "zh")
	if err := v.ResolveActions(node, &core.RenderContext{}); !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("未注入解析器应保留明确缺配置错误: %v", err)
	}
	ctx := &core.RenderContext{}
	want := core.ResolvedAction{Kind: core.ActionInstance, InstanceID: "frozen-checkout-b", PartKey: "checkout"}
	ctx.SetActionResolver(func(got *core.Node, key string) (core.ResolvedAction, error) {
		if got != node || key != "checkout" {
			t.Fatal("组件改写了动作解析输入")
		}
		return want, nil
	})
	if err := v.ResolveActions(node, ctx); err != nil || !v.HasCheckoutAction || v.CheckoutAction != want {
		t.Fatalf("组件应原样消费受控目标: %v %+v", err, v.CheckoutAction)
	}
}

// TestBuildViewIconFallback 图标名未知时回退默认图标（不做成空图标）。
func TestBuildViewIconFallback(t *testing.T) {
	v := BuildView(&Props{Icon: "not-a-real-icon-name"}, "proj-1", "zh-CN")
	if strings.TrimSpace(v.IconSVG) == "" {
		t.Fatal("未知图标名应回退默认购物车图标，而不是输出空")
	}
}

// TestApplyI18n 界面文案：默认标签与运行态文案取译文，作者自定义的标签不动。
//
// 判据是**收到的 key 集合**，不是「只收到标签那一个」：加载中文案是图标
// 自己产出的固定文案（作者填不到），与「作者是否自定义标签」各管一段 —— 实现在这两件事上
// 不许互相早退。所以这里同时钉两个方向：该取的都取了、不该取的一个也没多取。
func TestApplyI18n(t *testing.T) {
	v := &View{Label: textFallbackLabel}
	asked := map[string]string{}
	v.ApplyI18n(func(key, fallback string) string {
		asked[key] = fallback
		switch key {
		case TextKeyLabel:
			return "Cart"
		case TextKeyLoading:
			return "Loading…"
		}
		if !strings.HasPrefix(key, "site.fragment.cart.") && !strings.HasPrefix(key, "site.fragment.stock.") {
			t.Errorf("多取了文案键: %q", key)
		}
		return fallback
	})
	if v.Label != "Cart" {
		t.Fatalf("默认标签应取译文，实际 %q", v.Label)
	}
	if v.LoadingText != "Loading…" {
		t.Fatalf("运行态文案应取译文，实际 %q", v.LoadingText)
	}
	if len(asked) != 14 {
		t.Fatalf("无提示的正常渲染应包含静态购物车和图标的运行态文案，实际取了 %d 条: %v", len(asked), asked)
	}
	// 空值的降级提示不许被「无中生有」地填上：正常渲染的图标凭空多一行提示，
	// 与「缺站点工程」那条真降级混在一起，用户无从分辨。
	if v.Notice != "" {
		t.Fatalf("空提示不该被翻译函数填出内容，实际 %q", v.Notice)
	}

	// 作者自定义标签 → 标签那条不翻译（它属于内容，走 Translatable 链路）；运行态照旧。
	v2 := &View{Label: "我的车"}
	v2.ApplyI18n(func(key, fallback string) string {
		if key == TextKeyLabel {
			t.Error("作者自定义的标签不该走界面翻译（它由内容翻译链路负责）")
		}
		if key == TextKeyLoading {
			return "Loading…"
		}
		return fallback
	})
	if v2.Label != "我的车" {
		t.Fatalf("作者自定义的文案不该被界面翻译覆盖，实际 %q", v2.Label)
	}
	if v2.LoadingText != "Loading…" {
		t.Fatalf("自定义标签不该带走运行态文案的取词（两件事各自独立），实际 %q", v2.LoadingText)
	}

	// 非空的降级提示按 key 取词。
	v2b := &View{Label: textFallbackLabel, Notice: noticeNoProject}
	v2b.ApplyI18n(func(key, fallback string) string {
		if key == TextKeyNotice {
			return "Cart unavailable"
		}
		return fallback
	})
	if v2b.Notice != "Cart unavailable" {
		t.Fatalf("降级提示应取译文，实际 %q", v2b.Notice)
	}

	// 无翻译函数 → 全部保持中文兜底，不 panic。
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
