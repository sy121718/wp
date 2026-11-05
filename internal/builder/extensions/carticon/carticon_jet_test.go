package carticon

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/builder/core"
)

type cartIconTestData struct {
	Classes  string
	CustomID string
	NodeID   string
	V        View
}

func renderCartIconHTML(t *testing.T, p *Props) string {
	t.Helper()
	src, ok := core.OwnedTemplate("cart_icon")
	if !ok {
		t.Fatal("cart_icon 模板未在 core 注册（组件包 init 应调用 core.RegisterTemplate）")
	}
	loader := jet.NewInMemLoader()
	loader.Set("cart_icon", src)
	loader.Set("site_cart", siteCartTemplate)
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	tpl, err := set.GetTemplate("cart_icon")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	var buf bytes.Buffer
	data := cartIconTestData{
		Classes: "sky-c-n1",
		NodeID:  "n1",
		V:       BuildView(p, "proj-1", "zh-CN"),
	}
	if err := tpl.Execute(&buf, make(jet.VarMap), data); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return buf.String()
}

func TestCartIconJetHoverTouchToggleBindsPanel(t *testing.T) {
	html := renderCartIconHTML(t, &Props{Mode: ModeHover})

	if !strings.Contains(html, `type="checkbox" class="sky-cart-icon-toggle" id="sky-cart-icon-toggle-n1"`) {
		t.Errorf("触屏开合控件应是绑定到节点 id 的 checkbox\nHTML=%s", html)
	}

	if !strings.Contains(html, `class="sky-cart-icon-cover" for="sky-cart-icon-toggle-n1"`) {
		t.Errorf("覆盖层 label 未绑定到开合控件\nHTML=%s", html)
	}

	if !strings.Contains(html, `class="sky-cart-icon-toggle" id="sky-cart-icon-toggle-n1" aria-label="购物车"`) {
		t.Errorf("开合控件缺少可访问名（sr-only 控件读屏只能靠它）\nHTML=%s", html)
	}
}

func TestCartIconJetHoverToggleOrder(t *testing.T) {
	html := renderCartIconHTML(t, &Props{Mode: ModeHover})

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

func TestCartIconJetDetailsModesHaveNoTouchToggle(t *testing.T) {
	for _, mode := range []string{ModeDropdown, ModeDrawer, ModeModal} {
		html := renderCartIconHTML(t, &Props{Mode: mode})
		if strings.Contains(html, "sky-cart-icon-toggle") || strings.Contains(html, "sky-cart-icon-cover") {
			t.Errorf("形态 %s 不该出现触屏开合控件\nHTML=%s", mode, html)
		}
		if !strings.Contains(html, "data-cart-icon-panel") {
			t.Errorf("形态 %s 应保留原生可展开的 details 外壳\nHTML=%s", mode, html)
		}
	}
}

func TestCartIconHoverHasNoImplicitPageLink(t *testing.T) {
	html := renderCartIconHTML(t, &Props{Mode: ModeHover})
	if strings.Contains(html, `href="/cart"`) || strings.Contains(html, `href="#"`) {
		t.Fatal("出现隐式页面链接")
	}
	if !strings.Contains(html, `data-component-instance="n1"`) || !strings.Contains(html, `data-component-part="cart"`) {
		t.Fatal("缺少实例与区域声明")
	}
}
