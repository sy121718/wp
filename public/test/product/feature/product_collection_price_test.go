// Package feature product 模块 feature 测试 —— 价格区间筛选与价格排序（issue #28）。
//
// 价格在**变体**上（products 不存价格），所以区间筛选是 EXISTS 下推、排序按最低启用变体价：
// 这里在真实库上验证区间边界、非法输入拒绝、区间语义，以及集合项真的带出可排序的数值价格。
package feature

import (
	"context"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
)

// TestProductCollectionPriceRange 价格区间筛选（issue #28）。
func TestProductCollectionPriceRange(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)

	cheap := f.createProduct(t, "便宜货", "cheap", "", 50, 50)
	mid := f.createProduct(t, "中档货", "mid", "", 150, 150)
	pricey := f.createProduct(t, "贵货", "pricey", "", 300, 300)
	_ = cheap
	_ = mid
	_ = pricey

	ids := func(filter map[string]string) map[string]bool {
		t.Helper()
		items, rerr := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, filter)
		if rerr != nil {
			t.Fatalf("解析集合失败（%v）: %v", filter, rerr)
		}
		out := map[string]bool{}
		for _, it := range items {
			if name, ok := it["name"].(string); ok {
				out[name] = true
			}
		}
		return out
	}

	// 只有下限：≥ 100 → 中档与贵货。
	if got := ids(map[string]string{"minPrice": "100"}); len(got) != 2 || !got["中档货"] || !got["贵货"] {
		t.Fatalf("minPrice=100 应命中中档与贵货，实际 %v", got)
	}
	// 只有上限：≤ 100 → 只有便宜货。
	if got := ids(map[string]string{"maxPrice": "100"}); len(got) != 1 || !got["便宜货"] {
		t.Fatalf("maxPrice=100 应只命中便宜货，实际 %v", got)
	}
	// 闭区间且**含端**：100~150 → 只有中档（便宜货 50 在下限外）。
	if got := ids(map[string]string{"minPrice": "150", "maxPrice": "150"}); len(got) != 1 || !got["中档货"] {
		t.Fatalf("150~150 应只命中中档货（边界含端），实际 %v", got)
	}
	// 区间之外 → 空集合（不是报错）。
	if got := ids(map[string]string{"minPrice": "1000"}); len(got) != 0 {
		t.Fatalf("1000 以上应无命中，实际 %v", got)
	}
	// 与状态叠加：草稿商品不该被区间筛出来（先发布一个再看）。
	if got := ids(map[string]string{"status": "published", "minPrice": "0"}); len(got) != 0 {
		t.Fatalf("未发布商品不该出现在 published 筛选里，实际 %v", got)
	}

	// 非法输入一律报错：非数字 / 负数 / 下限大于上限。
	bad := []map[string]string{
		{"minPrice": "abc"},
		{"maxPrice": "-1"},
		{"minPrice": "300", "maxPrice": "100"},
	}
	for _, filter := range bad {
		if _, rerr := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, filter); rerr == nil {
			t.Fatalf("非法价格筛选 %v 应被拒绝", filter)
		}
	}
}

// TestProductCollectionItemCarriesPrice 集合项必须带**数值**价格（issue #28）。
//
// priceRange 是给人看的字符串（"99 ~ 199"），拿它排序会得到字典序；组件价格排序依赖 minPrice。
func TestProductCollectionItemCarriesPrice(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)
	f.createProduct(t, "带价商品", "with-price", "", 88, 188)

	items, err := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, map[string]string{})
	if err != nil {
		t.Fatalf("解析集合失败: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("应有商品")
	}
	for _, item := range items {
		price, ok := item["minPrice"].(float64)
		if !ok {
			t.Fatalf("集合项必须带数值 minPrice（价格排序要用），实际 %#v", item["minPrice"])
		}
		if price != 88 {
			t.Fatalf("minPrice 应取最低启用变体价 88，实际 %v", price)
		}
	}
}
