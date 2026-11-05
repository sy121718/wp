package templates

import (
	productdto "go_wp/internal/module/product/dto"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func tagHitsPageData() map[string]any {
	return taxonomyTags(&productdto.TagResp{ID: "t1", Name: "清仓", Slug: "clearance", Kind: "manual", ProductCount: 3})
}

func tagHitsPageDOM(t *testing.T, data map[string]any) (*html.Node, string) {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/product/product_tags", data, groupDRenderScope())
	if err != nil {
		t.Fatalf("标签页面渲染失败：%v", err)
	}
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	return root, out
}

func tagHitsPanel(t *testing.T, root *html.Node) *html.Node {
	t.Helper()
	panels := peAll(root, func(n *html.Node) bool { return peAttr(n, "id") == "tag-hits-panel" })
	if len(panels) != 1 {
		t.Fatalf("命中区域必须只有一个，实际%d", len(panels))
	}
	return panels[0]
}

func TestProductTagHitsPanelIsAnInitiallyHiddenLiveRegion(t *testing.T) {
	root, _ := tagHitsPageDOM(t, tagHitsPageData())
	panel := tagHitsPanel(t, root)
	if !peHasAttr(panel, "hidden") || !peHasAttr(panel, "data-product-tag-panel") || peAttr(panel, "role") != "region" || peAttr(panel, "aria-live") != "polite" {
		t.Fatalf("命中区域需要初始隐藏、控件挂载点与常驻播报区：%+v", panel.Attr)
	}
	labelID := peAttr(panel, "aria-labelledby")
	labels := peAll(panel, func(n *html.Node) bool { return labelID != "" && peAttr(n, "id") == labelID })
	if len(labels) != 1 || strings.TrimSpace(peText(labels[0])) == "" {
		t.Fatal("命中区域必须通过aria-labelledby关联可读标题")
	}
	status := peAll(panel, func(n *html.Node) bool { return peHasAttr(n, "data-tag-status") })
	if len(status) != 1 || peAttr(status[0], "role") != "status" || !peHasAttr(status[0], "hidden") {
		t.Fatal("读取失败必须有前端提示区域")
	}
	results := peAll(panel, func(n *html.Node) bool { return peHasAttr(n, "data-tag-result") })
	if len(results) != 1 || !peHasAttr(results[0], "hidden") {
		t.Fatal("首次请求前不能展示伪造命中结果")
	}
}

func TestProductTagHitsShellOwnsRowTemplatesAndEmptyTableStructure(t *testing.T) {
	root, _ := tagHitsPageDOM(t, tagHitsPageData())
	panel := tagHitsPanel(t, root)
	for _, attr := range []string{"data-tag-row", "data-tag-empty"} {
		tpls := peAll(panel, func(n *html.Node) bool { return peEl(n, "template") && peHasAttr(n, attr) })
		if len(tpls) != 1 {
			t.Fatalf("前端必须持有唯一%s模板", attr)
		}
	}
	headers := peAll(panel, func(n *html.Node) bool { return peEl(n, "th") })
	if len(headers) != 3 {
		t.Fatalf("命中列表表头必须常驻，实际%d列", len(headers))
	}
	rows := peAll(panel, func(n *html.Node) bool { return peEl(n, "tbody") && peHasAttr(n, "data-tag-rows") })
	if len(rows) != 1 || len(peKids(rows[0])) != 0 {
		t.Fatal("首次读取前商品行必须留空，数据只能来自JSON")
	}
	empty := peAll(panel, func(n *html.Node) bool { return peEl(n, "template") && peHasAttr(n, "data-tag-empty") })[0]
	cells := peAll(empty, func(n *html.Node) bool { return peEl(n, "td") })
	if len(cells) != 1 || peAttr(cells[0], "colspan") != "3" || !strings.Contains(peText(cells[0]), "当前没有商品命中这个标签") {
		t.Fatal("空态模板必须保留正确跨列和提示")
	}
}

func TestProductTagHitsUsesEntityDataWithoutServerDOMInstructions(t *testing.T) {
	root, out := tagHitsPageDOM(t, tagHitsPageData())
	buttons := peAll(root, func(n *html.Node) bool { return peEl(n, "button") && peHasAttr(n, "data-product-tag-hits") })
	if len(buttons) != 1 || peAttr(buttons[0], "data-product-tag-hits") != "t1" || peAttr(buttons[0], "data-project-id") != "pr1" || !strings.Contains(peText(buttons[0]), "3") {
		t.Fatal("读取入口必须带标签/工程与首屏计数事实")
	}
	for _, old := range []string{"/admin/product-tags/hits", `type="partial"`, `hx-swap-oob`, `hx-target="#tag-hits-panel"`} {
		if strings.Contains(out, old) {
			t.Fatalf("标签页面残留后端片段协议：%s", old)
		}
	}
	empty := tagHitsPageData()
	empty["Result"] = &productdto.TaxonomyListResp[*productdto.TagResp]{Items: []*productdto.TagResp{}, Page: 1, Size: 20}
	emptyRoot, _ := tagHitsPageDOM(t, empty)
	if !peHasAttr(tagHitsPanel(t, emptyRoot), "hidden") {
		t.Fatal("空标签列表不得展开命中面板")
	}
	if len(peAll(emptyRoot, func(n *html.Node) bool { return peHasAttr(n, "data-product-tag-hits") })) != 0 {
		t.Fatal("没有标签时不应提供命中读取入口")
	}
}
