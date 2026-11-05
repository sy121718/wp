package productlist

import (
	"errors"
	"go_wp/pkg/builder/core"
	"testing"
)

func listViewWithAction(t *testing.T, target *core.ActionTarget, resolved core.ResolvedAction, extra map[string]any) (View, error) {
	t.Helper()
	props := withFields(extra)
	p := decodePropsOf(t, props)
	node := nodeOf(t, props)
	ctx := &core.RenderContext{Collection: &fakeCollection{}}
	if target != nil {
		node.Actions = map[string]core.ActionTarget{"list": *target}
		ctx.SetActionResolver(func(got *core.Node, key string) (core.ResolvedAction, error) {
			if got != node || key != "list" {
				t.Fatal("必须消费此节点的显式list动作")
			}
			return resolved, nil
		})
	}
	return BuildView(node, &p, ctx)
}

func TestBuildViewListPageLinkUsesExplicitTarget(t *testing.T) {
	target := core.ActionTarget{Kind: core.ActionPage, PageID: "catalog-page"}
	view, err := listViewWithAction(t, &target, core.ResolvedAction{Kind: core.ActionPage, URL: "/zh/my-products"}, map[string]any{"listPageLink": "继续购物"})
	if err != nil || !view.HasListPageLink || view.ListPageLinkHref != "/zh/my-products" || view.ListPageLinkText != "继续购物" {
		t.Fatalf("只消费已解析目标与作者文案: %+v %v", view, err)
	}
	view, err = listViewWithAction(t, nil, core.ResolvedAction{}, nil)
	if err != nil || view.HasListPageLink || view.ListPageLinkHref != "" {
		t.Fatalf("无动作不得猜全站列表页: %+v %v", view, err)
	}
}

func TestBuildViewListActionRequiresResolver(t *testing.T) {
	props := withFields(nil)
	p := decodePropsOf(t, props)
	node := nodeOf(t, props)
	node.Actions = map[string]core.ActionTarget{"list": {Kind: core.ActionPage, PageID: "catalog-page"}}
	_, err := BuildView(node, &p, &core.RenderContext{Collection: &fakeCollection{}})
	if !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("未解析目标必须明确拒绝: %v", err)
	}
}
