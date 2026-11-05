package cardstack

import (
	"context"
	"fmt"
	"go_wp/pkg/builder/core"
	"testing"
)

type fakeProductCollection struct{}

func (fakeProductCollection) ResolveCollection(_ context.Context, source string, _ map[string]string) ([]map[string]any, error) {
	if source != "content:product" {
		return nil, fmt.Errorf("未知集合源 %q", source)
	}
	return []map[string]any{{"id": "p1", "name": "商品一", "slug": "p1"}}, nil
}
func articleProps() *Props {
	return &Props{CollectionSource: collectionSourceArticle, CardTitleField: "title", CardTextField: "excerpt", CardImageField: "featuredImage", CardLinkField: "slug", CardLinkPrefix: "/article/"}
}

func TestCollectionListPageLinkUsesExplicitTarget(t *testing.T) {
	for _, source := range []string{collectionSourceArticle, collectionSourceProduct} {
		t.Run(source, func(t *testing.T) {
			p := articleProps()
			p.CollectionSource = source
			p.ListPageLink = "查看完整目录"
			ctx := &core.RenderContext{Collection: fakeCollection{}}
			if source == collectionSourceProduct {
				p = &Props{CollectionSource: source, CardTitleField: "name", CardLinkField: "slug", ListPageLink: "查看完整目录"}
				ctx.Collection = fakeProductCollection{}
			}
			node := nodeOf(nil, 0)
			node.Actions = map[string]core.ActionTarget{"list": {Kind: core.ActionPage, PageID: "chosen-page"}}
			ctx.SetActionResolver(func(got *core.Node, key string) (core.ResolvedAction, error) {
				if got != node || key != "list" {
					t.Fatal("必须消费此实例的显式目标")
				}
				return core.ResolvedAction{Kind: core.ActionExternal, URL: "https://example.test/catalog", NewTab: true}, nil
			})
			view, err := BuildView(node, p, ctx)
			if err != nil || !view.HasListPageLink || view.ListPageLinkHref != "https://example.test/catalog" || view.ListPageLinkText != p.ListPageLink || !view.ListPageNewTab {
				t.Fatalf("显式目标不得被全站角色替换: %+v %v", view, err)
			}
			node.Actions = nil
			view, err = BuildView(node, p, ctx)
			if err != nil || view.HasListPageLink || view.ListPageLinkHref != "" {
				t.Fatalf("没有动作时不猜列表入口: %+v %v", view, err)
			}
		})
	}
}
