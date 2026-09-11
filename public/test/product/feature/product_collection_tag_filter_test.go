// Package feature product 模块 feature 测试 —— 多标签与在售筛选（issue #27）。
//
// 「热卖」「新品」这类筛选用**标签**表达，所以标签要支持多选（OR / AND 两种语义）；
// 「只看在售」与 #11 的 on_sale 自动标签同一判定（存在启用变体且划线价高于售价）。
package feature

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

// TestProductCollectionMultiTagAndOnSale 验收（issue #27）：
// 多标签 OR / AND、在售筛选、非法输入拒绝。
func TestProductCollectionMultiTagAndOnSale(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)

	hot := colTag(t, f, "热卖", "hot-sale")
	newer := colTag(t, f, "新品", "new-arrival")

	bothID := f.createProduct(t, "热卖新品", "hot-new", "", 99, 99)
	onlyHotID := f.createProduct(t, "只要热卖", "only-hot", "", 99, 99)
	onlyNewID := f.createProduct(t, "只要新品", "only-new", "", 99, 99)
	colAttach(t, f, bothID, nil, nil, []string{hot.ID, newer.ID})
	colAttach(t, f, onlyHotID, nil, nil, []string{hot.ID})
	colAttach(t, f, onlyNewID, nil, nil, []string{newer.ID})

	names := func(filter map[string]string) []string {
		t.Helper()
		items, rerr := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, filter)
		if rerr != nil {
			t.Fatalf("解析集合失败（%v）: %v", filter, rerr)
		}
		out := []string{}
		for _, it := range items {
			if name, ok := it["name"].(string); ok {
				out = append(out, name)
			}
		}
		return out
	}

	// OR（默认）：具备任一 → 三个都命中。
	if got := names(map[string]string{"tagIds": hot.ID + "," + newer.ID}); len(got) != 3 {
		t.Fatalf("OR 语义应命中 3 个，实际 %v", got)
	}
	// AND：同时具备全部 → 只有挂了两个标签的那个。
	and := names(map[string]string{"tagIds": hot.ID + "," + newer.ID, "tagMode": "all"})
	if len(and) != 1 || and[0] != "热卖新品" {
		t.Fatalf("AND 语义应只命中「热卖新品」，实际 %v", and)
	}
	// 单标签（多值维度的退化用法）。
	if got := names(map[string]string{"tagIds": hot.ID}); len(got) != 2 {
		t.Fatalf("单标签应命中 2 个，实际 %v", got)
	}

	// 在售：给「只要热卖」的变体设划线价（高于售价）→ 只有它命中。
	detail, derr := f.products.Get(ctx, &productdto.GetReq{ID: onlyHotID})
	if derr != nil {
		t.Fatalf("读商品失败: %v", derr)
	}
	compare := 199.0
	if _, uerr := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: detail.Variants[0].ID, ComparePrice: &compare,
	}); uerr != nil {
		t.Fatalf("设置划线价失败: %v", uerr)
	}
	onSale := names(map[string]string{"onSale": "true"})
	if len(onSale) != 1 || onSale[0] != "只要热卖" {
		t.Fatalf("只看在售应只命中设了划线价的那个，实际 %v", onSale)
	}
	// onSale=false 与不传等价（不限）。
	if got := names(map[string]string{"onSale": "false"}); len(got) != 3 {
		t.Fatalf("onSale=false 应不限，实际 %v", got)
	}

	// 非法输入必须报错（不伪装成空集合）。
	bad := []map[string]string{
		{"tagIds": "not-a-uuid"},
		{"tagIds": hot.ID + ",oops"},
		{"tagIds": hot.ID, "tagMode": "either"},
		{"onSale": "yes"},
	}
	for _, filter := range bad {
		if _, rerr := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, filter); rerr == nil {
			t.Fatalf("非法筛选 %v 应被拒绝", filter)
		} else if !strings.Contains(rerr.Error(), "tagIds") && !strings.Contains(rerr.Error(), "tagMode") && !strings.Contains(rerr.Error(), "onSale") {
			// 报错文案至少要能指向维度，别丢一个裸 SQL 错误出去
			t.Fatalf("报错应指向筛选维度，实际 %v", rerr)
		}
	}
}
