package productlist

// links_i18n_test.go — 列表卡片链接前缀的站内本地化（审计 I18N-015）。
//
// 只本地化**作者填的前缀**（/products/），CMS 字段值原样参与拼接：字段值可能是纯 slug、
// 完整路径或外链，后两种由 productcard.CardHref 原样返回，不该二次前缀 ——
// 与 button 的 ActionLink「CMS 绑定值不本地化」同一条口径。

import (
	"testing"

	"go_wp/internal/builder/core"
)

// siteLinkOf 模拟装配层注入的本地化器：只对站内路径加前缀。
func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

// TestBuildViewLocalizesCardLinkPrefix 集合模式：卡片详情地址带语言前缀。
func TestBuildViewLocalizesCardLinkPrefix(t *testing.T) {
	coll := &fakeCollection{items: []map[string]any{{"name": "衬衫", "slug": "shirt"}}}
	ctx := &core.RenderContext{Collection: coll}
	ctx.SetSiteLinkResolver(siteLinkOf)

	p := decodePropsOf(t, map[string]any{
		"collectionSource": "content:product",
		"titleField":       "item.name",
		"linkField":        "item.slug",
		"linkPrefix":       "/products/",
	})
	view, err := BuildView(nodeOf(t, nil), &p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Cards) != 1 || view.Cards[0].Href != "/en/products/shirt" {
		t.Fatalf("详情地址应带语言前缀，实际 %+v", view.Cards)
	}
}

// TestBuildViewWithoutSiteLinkKeepsHref 未注入本地化器时链接与接入前逐字一致。
func TestBuildViewWithoutSiteLinkKeepsHref(t *testing.T) {
	coll := &fakeCollection{items: []map[string]any{{"name": "衬衫", "slug": "shirt"}}}
	ctx := &core.RenderContext{Collection: coll}

	p := decodePropsOf(t, map[string]any{
		"collectionSource": "content:product",
		"titleField":       "item.name",
		"linkField":        "item.slug",
		"linkPrefix":       "/products/",
	})
	view, err := BuildView(nodeOf(t, nil), &p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Cards[0].Href != "/products/shirt" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", view.Cards[0].Href)
	}
}

// TestCardViewAbsoluteCMSValueUntouched 字段值本身是完整路径（CMS 给的）→ 不二次前缀。
func TestCardViewAbsoluteCMSValueUntouched(t *testing.T) {
	p := &Props{TitleField: "item.name", LinkField: "item.slug", LinkPrefix: "/products/"}
	c := cardViewOf(p, map[string]any{"name": "衬衫", "slug": "/products/shirt"}, siteLinkOf)
	if c.Href != "/products/shirt" {
		t.Fatalf("CMS 给的完整路径不该二次前缀，实际 %q", c.Href)
	}
}
