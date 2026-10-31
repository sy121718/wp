package builder

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

func TestLegacyPropsRenderButton(t *testing.T) {

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

	ip, err := core.DecodeProps[infobox.Props](json.RawMessage(
		`{"title":"标题","text":"正文","link":"/x","btnText":"了解更多"}`))
	if err != nil {
		t.Fatalf("infobox 解码失败: %v", err)
	}
	if ip.Label != "了解更多" {
		t.Errorf("core.infobox 未接旧名归一：Label=%q", ip.Label)
	}

	ap, err := core.DecodeProps[addtocart.Props](json.RawMessage(`{"buttonText":"立即购买"}`))
	if err != nil {
		t.Fatalf("addtocart 解码失败: %v", err)
	}
	if ap.Label != "立即购买" {
		t.Errorf("core.addtocart 未接旧名归一：Label=%q（作者自定义文案会变回默认值）", ap.Label)
	}

	lp, err := core.DecodeProps[articlelist.Props](json.RawMessage(`{"linkText":"阅读更多"}`))
	if err != nil {
		t.Fatalf("articlelist 解码失败: %v", err)
	}
	if lp.Label != "阅读更多" {
		t.Errorf("core.articlelist 未接旧名归一：Label=%q", lp.Label)
	}
}

func TestLegacyPropsCompilesEndToEnd(t *testing.T) {

	doc := json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[` +
		`{"id":"c1","type":"core.card","props":{"title":"T","text":"B",` +
		`"buttonText":"查看说明","buttonLink":"/a"}}]}`)
	page, err := ParsePage(doc)
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

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
