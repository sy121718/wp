package productlist

import (
	"testing"

	"go_wp/pkg/builder/core"
)

func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

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

func TestCardViewAbsoluteCMSValueUntouched(t *testing.T) {
	p := &Props{TitleField: "item.name", LinkField: "item.slug", LinkPrefix: "/products/"}
	c := cardViewOf(p, map[string]any{"name": "衬衫", "slug": "/products/shirt"}, siteLinkOf)
	if c.Href != "/products/shirt" {
		t.Fatalf("CMS 给的完整路径不该二次前缀，实际 %q", c.Href)
	}
}
