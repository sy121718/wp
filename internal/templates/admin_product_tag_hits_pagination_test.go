package templates

import (
	"testing"

	"golang.org/x/net/html"
)

// 分页行为由 product-read-controls.test.cjs 验证；模板只提供控件结构与本地化文案。
func TestProductTagHitsPaginationHasLocalControls(t *testing.T) {
	root, _ := tagHitsPageDOM(t, tagHitsPageData())
	panel := tagHitsPanel(t, root)
	nav := peAll(panel, func(n *html.Node) bool { return peEl(n, "nav") && peClass(n, "pagination") })
	if len(nav) != 1 || peAttr(nav[0], "aria-label") == "" {
		t.Fatal("分页必须提供有区域名的导航")
	}
	buttons := peAll(nav[0], func(n *html.Node) bool { return peEl(n, "button") && peHasAttr(n, "data-tag-page") })
	if len(buttons) != 2 {
		t.Fatalf("分页应有上一页和下一页，实际%d个", len(buttons))
	}
	for i, offset := range []string{"-1", "1"} {
		button := buttons[i]
		if peAttr(button, "data-tag-page") != offset || peAttr(button, "type") != "button" || peText(button) == "" {
			t.Fatalf("分页按钮缺少本地控件契约：%+v", button.Attr)
		}
		if peHasAttr(button, "hx-get") || peHasAttr(button, "hx-target") {
			t.Fatal("分页不得再请求HTML片段")
		}
	}
	info := peAll(nav[0], func(n *html.Node) bool { return peHasAttr(n, "data-tag-page-info") })
	if len(info) != 1 || peText(info[0]) != "" {
		t.Fatal("页码提示必须由当前JSON的总数和实际行数填入")
	}
	if peAttr(panel, "data-page-info") == "" {
		t.Fatal("分页提示文案必须由模板取词")
	}
}
