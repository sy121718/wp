package productlist

// productlist_slot_test.go — 商品列表的「全部商品」入口单测（BIZ-2）。
//
// 槽位语义：槽位 → 当前语言线上路径，且**只含已绑定且已发布**的页面。
// 组件只负责「有路径就输出、没有就不输出」—— 绝不猜默认路径（猜错就是死链，
// 而页面作者从产物上看不出它是猜的）。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// listViewWithPages 用给定的槽位表构建列表视图（集合桩返回空集合，链接与数据无关）。
func listViewWithPages(t *testing.T, pages map[string]string, extra map[string]any) View {
	t.Helper()
	props := withFields(extra)
	p := decodePropsOf(t, props)
	ctx := &core.RenderContext{Collection: &fakeCollection{}, SitePages: pages}
	view, err := BuildView(nodeOf(t, props), &p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	return view
}

// TestBuildViewListPageLinkFollowsSiteSlot 只有 shop 槽位给了路径才输出入口。
func TestBuildViewListPageLinkFollowsSiteSlot(t *testing.T) {
	view := listViewWithPages(t, map[string]string{"shop": "/shop/"}, nil)
	if !view.HasListPageLink || view.ListPageLinkHref != "/shop/" {
		t.Fatalf("应按 shop 槽位输出链接，got has=%v href=%q", view.HasListPageLink, view.ListPageLinkHref)
	}
	if view.ListPageLinkText != defaultListPageLinkText {
		t.Errorf("缺省文案应为 %q，实际 %q", defaultListPageLinkText, view.ListPageLinkText)
	}
}

// TestBuildViewListPageLinkDegrades 槽位未绑定 / 未发布 / 只有别的槽位：不输出链接。
func TestBuildViewListPageLinkDegrades(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pages map[string]string
	}{
		{"槽位表为空", map[string]string{}},
		{"nil 槽位表", nil},
		{"只有别的槽位", map[string]string{"blog": "/blog/", "cart": "/cart/"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := listViewWithPages(t, tc.pages, nil)
			if view.HasListPageLink || strings.TrimSpace(view.ListPageLinkHref) != "" {
				t.Fatalf("没有 shop 路径时不得输出链接（死链），got has=%v href=%q",
					view.HasListPageLink, view.ListPageLinkHref)
			}
			if strings.TrimSpace(view.ListPageLinkText) != "" {
				t.Fatalf("不输出链接时也不该留下文案，got %q", view.ListPageLinkText)
			}
		})
	}
}

// TestBuildViewListPageLinkAuthorText 作者可以改文案（「继续购物」等），目标仍来自槽位。
func TestBuildViewListPageLinkAuthorText(t *testing.T) {
	view := listViewWithPages(t, map[string]string{"shop": "/shop/"}, map[string]any{"listPageLink": "继续购物"})
	if view.ListPageLinkText != "继续购物" {
		t.Errorf("作者文案应生效，实际 %q", view.ListPageLinkText)
	}
	if view.ListPageLinkHref != "/shop/" {
		t.Errorf("目标应来自槽位，实际 %q", view.ListPageLinkHref)
	}
}
