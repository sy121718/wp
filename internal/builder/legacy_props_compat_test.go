package builder

// legacy_props_compat_test.go — 组件旧 props 名的**端到端**兼容（改名不迁移数据）。
//
// 背景：这一批把 buttonText / btnText / linkText 统一成 label。存量文档里存的是旧键，
// 不做归一时 BuildView 的「有无按钮」判断为假 —— 整颗 <a> **静默消失**（不报错、不 500）。
//
// 本仓实测影响面（修复前）：
//
//	blueprints            2 张（core.card × 9 处）
//	content_templates     1 张（商品详情页）
//	document_snapshots    57888 条含旧键
//	受影响已发布实例       97 个（全部走商品详情页模板）
//
// 与 core/props_legacy_test.go 的分工：那里测归一的**语义**（新值优先、空串等价缺省…），
// 这里测**每个真实组件都接上了**归一 —— 漏接一个组件，那个组件的按钮就静默消失。

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder/components/addtocart"
	"go_wp/internal/builder/components/articlelist"
	"go_wp/internal/builder/components/card"
	"go_wp/internal/builder/components/infobox"
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// TestLegacyPropsRenderButton 四个改过名的组件：旧键文档必须还能渲染出按钮/文案。
func TestLegacyPropsRenderButton(t *testing.T) {
	// card：buttonText → label
	p, err := core.DecodeProps[card.Props](json.RawMessage(
		`{"title":"标题","text":"正文","buttonText":"查看详情","buttonLink":"/about"}`))
	if err != nil {
		t.Fatalf("card 解码失败: %v", err)
	}
	if p.Label != "查看详情" {
		t.Errorf("core.card 未接旧名归一：Label=%q（存量文档的按钮会整颗消失）", p.Label)
	}
	if !card.BuildView(&p, nil).HasButton {
		t.Error("core.card 旧键文档渲染后 HasButton=false —— 按钮消失")
	}

	// infobox：btnText → label
	ip, err := core.DecodeProps[infobox.Props](json.RawMessage(
		`{"title":"标题","text":"正文","link":"/x","btnText":"了解更多"}`))
	if err != nil {
		t.Fatalf("infobox 解码失败: %v", err)
	}
	if ip.Label != "了解更多" {
		t.Errorf("core.infobox 未接旧名归一：Label=%q", ip.Label)
	}

	// addtocart：buttonText → label
	ap, err := core.DecodeProps[addtocart.Props](json.RawMessage(`{"buttonText":"立即购买"}`))
	if err != nil {
		t.Fatalf("addtocart 解码失败: %v", err)
	}
	if ap.Label != "立即购买" {
		t.Errorf("core.addtocart 未接旧名归一：Label=%q（作者自定义文案会变回默认值）", ap.Label)
	}

	// articlelist：linkText → label
	lp, err := core.DecodeProps[articlelist.Props](json.RawMessage(`{"linkText":"阅读更多"}`))
	if err != nil {
		t.Fatalf("articlelist 解码失败: %v", err)
	}
	if lp.Label != "阅读更多" {
		t.Errorf("core.articlelist 未接旧名归一：Label=%q", lp.Label)
	}
}

// TestLegacyPropsCompilesEndToEnd 整页编译：旧键文档产出的 HTML 里必须有按钮。
//
// 单测解码还不够 —— 编译链路上还有几个入口（children 递归、容器子节点）。
// 这条走真实 Compile，断言产物。
func TestLegacyPropsCompilesEndToEnd(t *testing.T) {
	// settings.layout.mode 是必填（validateSettings 拒绝空值）。
	doc := json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[` +
		`{"id":"c1","type":"core.card","props":{"title":"T","text":"B",` +
		`"buttonText":"查看说明","buttonLink":"/a"}}]}`)
	page, err := ParsePage(doc)
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	// 必须注入真实模板 Set：这条要验证的是「产物里有没有按钮」，走桩就测不到渲染。
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	got, err := Compile(page, WithComponentSet(set))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(got.HTML, "查看说明") {
		t.Errorf("编译产物里没有旧键的按钮文案 —— 存量蓝图会丢按钮：\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, "sky-card-btn") {
		t.Errorf("编译产物里没有按钮元素：\n%s", got.HTML)
	}
}
