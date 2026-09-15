package cardstack

// cardstack_slot_test.go — 集合列表的「列表页入口」单测（BIZ-2）。
//
// 槽位语义：槽位 → 当前语言线上路径，且**只含已绑定且已发布**的页面。
// 因此这里要钉住的是三件事：
//
//	1. 文章集合 → blog 槽位（缺省文案「更多文章」）；
//	2. 商品集合 → shop 槽位（缺省文案「全部商品」）；
//	3. 槽位没绑 / 没发布 / 集合源对不上系统页面语义 → **不输出链接**（绝不猜路径）。

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// fakeProductCollection 商品集合桩：只返回一条，用于验证 shop 槽位映射。
type fakeProductCollection struct{}

func (fakeProductCollection) ResolveCollection(_ context.Context, source string, _ map[string]string) ([]map[string]any, error) {
	if source != "content:product" {
		return nil, fmt.Errorf("未知集合源 %q", source)
	}
	return []map[string]any{{"id": "p1", "name": "商品一", "slug": "p1"}}, nil
}

// articleProps 集合字段映射（与 TestCollectionCards 同一组）。
func articleProps() *Props {
	return &Props{
		CollectionSource: collectionSourceArticle,
		CardTitleField:   "title",
		CardTextField:    "excerpt",
		CardImageField:   "featuredImage",
		CardLinkField:    "slug",
		CardLinkPrefix:   "/article/",
	}
}

// TestCollectionListPageLinkFollowsSiteSlot 文章集合按 blog 槽位取路径。
func TestCollectionListPageLinkFollowsSiteSlot(t *testing.T) {
	p := articleProps()
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	ctx.SetSitePages(map[string]string{siteSlotBlog: "/blog/"})
	view, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !view.HasListPageLink || view.ListPageLinkHref != "/blog/" {
		t.Fatalf("应按 blog 槽位输出链接，got has=%v href=%q", view.HasListPageLink, view.ListPageLinkHref)
	}
	if view.ListPageLinkText != defaultArticleMoreText {
		t.Errorf("缺省文案应为 %q，实际 %q", defaultArticleMoreText, view.ListPageLinkText)
	}
}

// TestCollectionListPageLinkAuthorText 作者只能改文案，改不了目标。
func TestCollectionListPageLinkAuthorText(t *testing.T) {
	p := articleProps()
	p.ListPageLink = "看全部文章"
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	ctx.SetSitePages(map[string]string{siteSlotBlog: "/blog/"})
	view, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.ListPageLinkText != "看全部文章" {
		t.Errorf("作者文案应生效，实际 %q", view.ListPageLinkText)
	}
	if view.ListPageLinkHref != "/blog/" {
		t.Errorf("目标仍应来自槽位，实际 %q", view.ListPageLinkHref)
	}
}

// TestCollectionListPageLinkDegrades 槽位缺失 / 未发布 / 集合源无对应系统页面 → 不输出链接。
func TestCollectionListPageLinkDegrades(t *testing.T) {
	cases := []struct {
		name  string
		p     *Props
		pages map[string]string
	}{
		{"槽位未绑定或未发布", articleProps(), map[string]string{}},
		{"只有别的槽位", articleProps(), map[string]string{siteSlotShop: "/shop/"}},
		{"nil 槽位表", articleProps(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &core.RenderContext{Collection: fakeCollection{}}
			ctx.SetSitePages(tc.pages)
			view, err := BuildView(nodeOf(nil, 0), tc.p, ctx)
			if err != nil {
				t.Fatalf("BuildView: %v", err)
			}
			if view.HasListPageLink || strings.TrimSpace(view.ListPageLinkHref) != "" {
				t.Fatalf("槽位没给路径时不得输出链接（死链），got has=%v href=%q",
					view.HasListPageLink, view.ListPageLinkHref)
			}
		})
	}

	// 商品集合在商品槽位缺失时同理。
	pp := &Props{CollectionSource: collectionSourceProduct, CollectionLimit: 1}
	ctx := &core.RenderContext{Collection: fakeProductCollection{}}
	ctx.SetSitePages(map[string]string{})
	view, err := BuildView(nodeOf(nil, 0), pp, ctx)
	if err != nil {
		t.Fatalf("BuildView(product): %v", err)
	}
	if view.HasListPageLink {
		t.Fatalf("商品集合在没有 shop 槽位时不该输出链接")
	}
}

// TestCollectionListPageLinkProductSlot 商品集合按 shop 槽位取路径，缺省文案「全部商品」。
func TestCollectionListPageLinkProductSlot(t *testing.T) {
	p := &Props{CollectionSource: collectionSourceProduct, CollectionLimit: 1}
	ctx := &core.RenderContext{Collection: fakeProductCollection{}}
	ctx.SetSitePages(map[string]string{siteSlotShop: "/products/"})
	view, err := BuildView(nodeOf(nil, 0), p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !view.HasListPageLink || view.ListPageLinkHref != "/products/" {
		t.Fatalf("应按 shop 槽位输出链接，got has=%v href=%q", view.HasListPageLink, view.ListPageLinkHref)
	}
	if view.ListPageLinkText != defaultProductMoreText {
		t.Errorf("缺省文案应为 %q，实际 %q", defaultProductMoreText, view.ListPageLinkText)
	}
}

// TestCollectionListPageLinkNonCollection 静态卡片（非集合）不输出列表页入口：
// 它不是「列表」，给列表页入口没有语义。
func TestCollectionListPageLinkNonCollection(t *testing.T) {
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	ctx.SetSitePages(map[string]string{siteSlotBlog: "/blog/"})
	view, err := BuildView(nodeOf(nil, 3), &Props{}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.HasListPageLink {
		t.Fatalf("非集合模式不该输出列表页入口")
	}
}
