package carticon

// carticon_jet_test.go — 购物车图标模板的结构契约。
//
// hover 形态在触屏上靠 label + checkbox 驱动（见 carticon.css 的 @hovernone 块），
// 而 label 与控件之间只靠 for/id 绑定，CSS 侧用兄弟选择器（~）控制浮层显隐：
// id 写错、写重，或元素顺序颠倒，浏览器一律不报错 —— 只在触屏上表现为「点了没反应」，
// 那正是本条目要修的功能缺失。所以这层结构在这里钉死。

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder/core"
)

// cartIconTestData 模板渲染上下文（与 jetview 侧 nodeView 同形：Classes/CustomID/NodeID/V）。
type cartIconTestData struct {
	Classes  string
	CustomID string
	NodeID   string
	V        View
}

// renderCartIconHTML 用组件自带的模板源码渲染一次购物车图标，返回产物 HTML。
func renderCartIconHTML(t *testing.T, p *Props, cartURL string) string {
	t.Helper()
	src, ok := core.OwnedTemplate("cart_icon")
	if !ok {
		t.Fatal("cart_icon 模板未在 core 注册（组件包 init 应调用 core.RegisterTemplate）")
	}
	loader := jet.NewInMemLoader()
	loader.Set("cart_icon", src)
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	tpl, err := set.GetTemplate("cart_icon")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	var buf bytes.Buffer
	data := cartIconTestData{
		Classes: "sky-c-n1",
		NodeID:  "n1",
		V:       BuildView(p, "proj-1", "zh-CN", cartURL),
	}
	if err := tpl.Execute(&buf, make(jet.VarMap), data); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return buf.String()
}

// TestCartIconJetHoverTouchToggleBindsPanel hover 形态要渲染出成对的触屏开合控件。
//
// 没有这对控件，触屏上就没有任何办法展开浮层（:hover 永不触发）——
// 这不是样式差异，是「购物车图标点了没反应」。
func TestCartIconJetHoverTouchToggleBindsPanel(t *testing.T) {
	html := renderCartIconHTML(t, &Props{Mode: ModeHover}, "/cart")

	// 必须是 checkbox：radio 点不掉，触屏展开后再也收不回去。
	if !strings.Contains(html, `type="checkbox" class="sky-cart-icon-toggle" id="sky-cart-icon-toggle-n1"`) {
		t.Errorf("触屏开合控件应是绑定到节点 id 的 checkbox\nHTML=%s", html)
	}
	// 覆盖层靠 for 绑到同一个控件上，写错就是「点了没反应」。
	if !strings.Contains(html, `class="sky-cart-icon-cover" for="sky-cart-icon-toggle-n1"`) {
		t.Errorf("覆盖层 label 未绑定到开合控件\nHTML=%s", html)
	}
	// 控件自身是 sr-only 的，读屏与键盘用户只能靠可访问名识别它，不能为空。
	if !strings.Contains(html, `class="sky-cart-icon-toggle" id="sky-cart-icon-toggle-n1" aria-label="购物车"`) {
		t.Errorf("开合控件缺少可访问名（sr-only 控件读屏只能靠它）\nHTML=%s", html)
	}
}

// TestCartIconJetHoverToggleOrder 控件必须排在图标链接之后、浮层之前。
//
// CSS 侧用兄弟选择器（~）驱动浮层显隐与覆盖层接管点击，顺序错了选择器一律不命中，
// 而且产物看起来完全正常 —— 只有触屏上点不动才发现。
func TestCartIconJetHoverToggleOrder(t *testing.T) {
	html := renderCartIconHTML(t, &Props{Mode: ModeHover}, "/cart")

	toggle := strings.Index(html, `class="sky-cart-icon-toggle"`)
	link := strings.Index(html, `class="sky-cart-icon-trigger"`)
	cover := strings.Index(html, `class="sky-cart-icon-cover"`)
	panel := strings.Index(html, `class="sky-cart-icon-panel"`)

	if toggle < 0 || link < 0 || cover < 0 || panel < 0 {
		t.Fatalf("结构不完整（控件 %d / 链接 %d / 覆盖层 %d / 浮层 %d）\nHTML=%s",
			toggle, link, cover, panel, html)
	}
	if !(toggle < link && link < cover && cover < panel) {
		t.Errorf("顺序应为 控件 < 图标链接 < 覆盖层 < 浮层（实际 %d/%d/%d/%d）\nHTML=%s",
			toggle, link, cover, panel, html)
	}
}

// TestCartIconJetDetailsModesHaveNoTouchToggle 其余三种形态走原生 <details>，
// 不该混进 checkbox —— 两套开合机制并存只会互相打架（键盘停靠点也白白多一个）。
func TestCartIconJetDetailsModesHaveNoTouchToggle(t *testing.T) {
	for _, mode := range []string{ModeDropdown, ModeDrawer, ModeModal} {
		html := renderCartIconHTML(t, &Props{Mode: mode}, "/cart")
		if strings.Contains(html, "sky-cart-icon-toggle") || strings.Contains(html, "sky-cart-icon-cover") {
			t.Errorf("形态 %s 不该出现触屏开合控件\nHTML=%s", mode, html)
		}
		if !strings.Contains(html, "data-cart-icon-panel") {
			t.Errorf("形态 %s 应保留原生可展开的 details 外壳\nHTML=%s", mode, html)
		}
	}
}

// TestCartIconJetSlotLinkSurvives 有购物车槽位时，桌面那条「点图标去购物车页」的链接
// 必须原样保留：触屏覆盖层是纯 CSS 的 @hovernone 规则，改不动 DOM，
// 所以链接不能因为加了触屏交互而被换掉。
func TestCartIconJetSlotLinkSurvives(t *testing.T) {
	html := renderCartIconHTML(t, &Props{Mode: ModeHover}, "/cart")
	if !strings.Contains(html, `class="sky-cart-icon-trigger" href="/cart"`) {
		t.Errorf("hover 形态的图标链接被改动了（桌面点击应仍是原行为）\nHTML=%s", html)
	}
}
