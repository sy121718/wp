package nav

// nav_jet_test.go — 导航模板的结构契约。
//
// 触屏子菜单靠 label + checkbox 驱动（见 nav.css 的 @hovernone 块），
// 而 label 与控件之间只靠 for/id 绑定：id 写错、写重、或元素顺序颠倒，
// 浏览器一律不报错，只是在触屏上「点了没反应」——那正是本条目要修的功能缺失。
// 所以这层结构在这里钉死。

import (
	"bytes"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder/core"
)

// navTestData 模板渲染上下文（与 jetview 侧 navView 同形：Classes/NodeID/V）。
type navTestData struct {
	Classes string
	NodeID  string
	V       View
}

// renderNavHTML 用组件自带的模板源码渲染一次导航，返回产物 HTML。
func renderNavHTML(t *testing.T, v View) string {
	t.Helper()
	src, ok := core.OwnedTemplate("nav")
	if !ok {
		t.Fatal("nav 模板未在 core 注册（组件包 init 应调用 core.RegisterTemplate）")
	}
	loader := jet.NewInMemLoader()
	loader.Set("nav", src)
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	tpl, err := set.GetTemplate("nav")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, make(jet.VarMap), navTestData{Classes: "sky-c-n1", NodeID: "n1", V: v}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return buf.String()
}

// TestNavJetSubmenuToggleBindsToParent 有子项的父项要渲染出成对的开关，
// 且 id 与两个 label 的 for 完全一致。
func TestNavJetSubmenuToggleBindsToParent(t *testing.T) {
	v := BuildView(&core.Node{ID: "n1"}, &Props{Items: []Item{
		{Label: "关于", URL: "/about", Children: []Item{
			{Label: "团队", URL: "/team"},
			{Label: "联系", URL: "/contact"},
		}},
	}})
	html := renderNavHTML(t, v)

	for _, want := range []string{
		`id="sky-nav-sub-n1-0"`,
		`class="sky-nav-sub-cover" for="sky-nav-sub-n1-0"`,
		`class="sky-nav-sub-chevron" for="sky-nav-sub-n1-0"`,
		`aria-label="关于 子菜单"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("产物缺少 %q\nHTML=%s", want, html)
		}
	}
	// 控件必须是 checkbox：radio 点不掉，展开后就再也收不回去。
	if !strings.Contains(html, `<input type="checkbox" class="sky-nav-sub-toggle"`) {
		t.Errorf("触屏开关应是 checkbox（radio 无法取消选中）\nHTML=%s", html)
	}
}

// TestNavJetSubmenuToggleOrder 开关必须排在父链接之后、子菜单之前。
//
// CSS 侧用兄弟选择器（~）驱动展开与覆盖层撤除，顺序错了选择器一律不命中，
// 而且产物看起来完全正常 —— 只有触屏上点不动才发现。
func TestNavJetSubmenuToggleOrder(t *testing.T) {
	v := BuildView(&core.Node{ID: "n1"}, &Props{Items: []Item{
		{Label: "关于", URL: "/about", Children: []Item{{Label: "团队", URL: "/team"}}},
	}})
	html := renderNavHTML(t, v)

	link := strings.Index(html, `>关于</a>`)
	toggle := strings.Index(html, `class="sky-nav-sub-toggle"`)
	cover := strings.Index(html, `class="sky-nav-sub-cover"`)
	chevron := strings.Index(html, `class="sky-nav-sub-chevron"`)
	sub := strings.Index(html, `class="sky-nav-sub"`)

	if link < 0 || toggle < 0 || cover < 0 || chevron < 0 || sub < 0 {
		t.Fatalf("结构不完整（链接 %d / 开关 %d / 覆盖层 %d / 箭头 %d / 子菜单 %d）\nHTML=%s",
			link, toggle, cover, chevron, sub, html)
	}
	if !(link < toggle && toggle < cover && cover < chevron && chevron < sub) {
		t.Errorf("顺序应为 父链接 < 开关 < 覆盖层 < 箭头 < 子菜单（实际 %d/%d/%d/%d/%d）\nHTML=%s",
			link, toggle, cover, chevron, sub, html)
	}
}

// TestNavJetSubmenuToggleOnlyOnParents 没有子项的菜单项不渲染开关：
// 多余的控件既占键盘停靠点，也是一层无意义的点击覆盖。
func TestNavJetSubmenuToggleOnlyOnParents(t *testing.T) {
	v := BuildView(&core.Node{ID: "n1"}, &Props{Items: []Item{
		{Label: "首页", URL: "/"},
		{Label: "关于", URL: "/about", Children: []Item{{Label: "团队", URL: "/team"}}},
		{Label: "博客", URL: "/blog"},
	}})
	html := renderNavHTML(t, v)

	if n := strings.Count(html, `type="checkbox" class="sky-nav-sub-toggle"`); n != 1 {
		t.Errorf("只有带子项的那一项该有开关，实际 %d 个\nHTML=%s", n, html)
	}
	if n := strings.Count(html, `class="sky-nav-sub-cover"`); n != 1 {
		t.Errorf("覆盖层应只出现在带子项的项上，实际 %d 个", n)
	}
	if n := strings.Count(html, `class="sky-nav-sub-chevron"`); n != 1 {
		t.Errorf("箭头应只出现在带子项的项上，实际 %d 个", n)
	}
}

// TestNavJetSubmenuToggleIDsUnique 多个父项的 id 必须互不相同：
// 重了会让所有 label 一起切同一个控件，点是分不清哪一项展开。
func TestNavJetSubmenuToggleIDsUnique(t *testing.T) {
	v := BuildView(&core.Node{ID: "n1"}, &Props{Items: []Item{
		{Label: "产品", URL: "/p", Children: []Item{{Label: "甲", URL: "/p/a"}}},
		{Label: "方案", URL: "/s", Children: []Item{{Label: "乙", URL: "/s/a"}}},
		{Label: "支持", URL: "/h", Children: []Item{{Label: "丙", URL: "/h/a"}}},
	}})
	html := renderNavHTML(t, v)

	for _, id := range []string{"sky-nav-sub-n1-0", "sky-nav-sub-n1-1", "sky-nav-sub-n1-2"} {
		if !strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("缺少开关 id %q\nHTML=%s", id, html)
		}
		// 每条 id 都要被两个 label（覆盖层 + 箭头）引用。
		if n := strings.Count(html, `for="`+id+`"`); n != 2 {
			t.Errorf("id %q 应被覆盖层与箭头各引用一次，实际 %d 次\nHTML=%s", id, n, html)
		}
	}
}

// TestNavJetMobileCollapseUntouched 折叠开关（移动端汉堡菜单）不在本次改动范围内，
// 顺带钉住它还在：两条开关机制共存，互不干扰。
func TestNavJetMobileCollapseUntouched(t *testing.T) {
	v := BuildView(&core.Node{ID: "n1"}, &Props{
		MobileCollapse: true,
		Items:          []Item{{Label: "首页", URL: "/"}},
	})
	html := renderNavHTML(t, v)

	for _, want := range []string{
		`class="sky-nav-toggle" id="sky-nav-toggle-n1"`,
		`class="sky-nav-burger" for="sky-nav-toggle-n1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("移动端折叠开关被改动了，缺少 %q\nHTML=%s", want, html)
		}
	}
}
