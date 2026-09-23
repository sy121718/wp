// Package feature product 模块 feature 测试 —— 多值筛选维度（分类 / 品牌 / 标签同一套样板）。
//
// 三个维度的形状完全一致：逗号分隔的 id 串 + any（默认，并集）/ all（交集）语义，
// 且与各自单值维度并存时**多值优先**。这里逐条钉住语义，因为它们错了不会报错，
// 只会少出商品（「勾了两个品牌只出第一个的」）。
package feature

import (
	"context"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
)

// TestProductCollectionMultiCategoryAndBrand 多分类 / 多品牌筛选语义（并集 / 交集 / 多值优先）。
func TestProductCollectionMultiCategoryAndBrand(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)

	catA := colCategory(t, f, "上衣", "multi-tops")
	catB := colCategory(t, f, "裤子", "multi-trousers")
	brandA := colBrand(t, f, "品牌甲", "multi-brand-a")
	brandB := colBrand(t, f, "品牌乙", "multi-brand-b")

	// 四个商品：A 类甲牌 / B 类乙牌 / 同时属两类（甲牌） / 两类都不是
	onlyA := f.createProduct(t, "只要上衣", "mv-only-a", "", 10, 10)
	onlyB := f.createProduct(t, "只要裤子", "mv-only-b", "", 10, 10)
	bothCats := f.createProduct(t, "两者都算", "mv-both-cats", "", 10, 10)
	neither := f.createProduct(t, "都不算", "mv-neither", "", 10, 10)
	colAttach(t, f, onlyA, []string{catA.ID}, &brandA.ID, nil)
	colAttach(t, f, onlyB, []string{catB.ID}, &brandB.ID, nil)
	colAttach(t, f, bothCats, []string{catA.ID, catB.ID}, &brandA.ID, nil)
	colAttach(t, f, neither, nil, nil, nil)

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
	has := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		set := map[string]bool{}
		for _, g := range got {
			set[g] = true
		}
		for _, w := range want {
			if !set[w] {
				return false
			}
		}
		return true
	}

	// 多分类并集（any，默认）。
	if got := names(map[string]string{"categoryIds": catA.ID + "," + catB.ID}); !has(got, "只要上衣", "只要裤子", "两者都算") {
		t.Fatalf("多分类并集应命中三个，实际 %v", got)
	}
	// 多分类交集（all）。
	if got := names(map[string]string{"categoryIds": catA.ID + "," + catB.ID, "categoryMode": "all"}); !has(got, "两者都算") {
		t.Fatalf("多分类交集应只命中「两者都算」，实际 %v", got)
	}
	// 多品牌并集。
	if got := names(map[string]string{"brandIds": brandA.ID + "," + brandB.ID}); !has(got, "只要上衣", "只要裤子", "两者都算") {
		t.Fatalf("多品牌并集应命中三个，实际 %v", got)
	}
	// 多品牌交集：没有商品同时挂两个品牌。
	if got := names(map[string]string{"brandIds": brandA.ID + "," + brandB.ID, "brandMode": "all"}); len(got) != 0 {
		t.Fatalf("多品牌交集应为空，实际 %v", got)
	}

	// **多值优先于单值**：两者并存时按多值算，单值必须被忽略。
	// 叠加成 AND 的话，用户把单选改成多选之后旧值会继续卡着结果。
	if got := names(map[string]string{"categoryId": catB.ID, "categoryIds": catA.ID}); !has(got, "只要上衣", "两者都算") {
		t.Fatalf("多值应压过单值（应出 A 类的两个），实际 %v", got)
	}
	if got := names(map[string]string{"brandId": brandB.ID, "brandIds": brandA.ID}); !has(got, "只要上衣", "两者都算") {
		t.Fatalf("品牌多值应压过单值（应出甲牌的两个），实际 %v", got)
	}

	// 空值 = 该维度不参与过滤（留一个空键必须等价于不传，不能变成 0 条）。
	if got := names(map[string]string{"categoryIds": ""}); has(got, "只要上衣", "只要裤子", "两者都算", "都不算") == false {
		t.Fatalf("空 categoryIds 应等价于不筛选，实际 %v", got)
	}
}
