package feature

import (
	"context"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
)

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

	if got := names(map[string]string{"categoryIds": catA.ID + "," + catB.ID}); !has(got, "只要上衣", "只要裤子", "两者都算") {
		t.Fatalf("多分类并集应命中三个，实际 %v", got)
	}

	if got := names(map[string]string{"categoryIds": catA.ID + "," + catB.ID, "categoryMode": "all"}); !has(got, "两者都算") {
		t.Fatalf("多分类交集应只命中「两者都算」，实际 %v", got)
	}

	if got := names(map[string]string{"brandIds": brandA.ID + "," + brandB.ID}); !has(got, "只要上衣", "只要裤子", "两者都算") {
		t.Fatalf("多品牌并集应命中三个，实际 %v", got)
	}

	if got := names(map[string]string{"brandIds": brandA.ID + "," + brandB.ID, "brandMode": "all"}); len(got) != 0 {
		t.Fatalf("多品牌交集应为空，实际 %v", got)
	}

	if got := names(map[string]string{"categoryId": catB.ID, "categoryIds": catA.ID}); !has(got, "只要上衣", "两者都算") {
		t.Fatalf("多值应压过单值（应出 A 类的两个），实际 %v", got)
	}
	if got := names(map[string]string{"brandId": brandB.ID, "brandIds": brandA.ID}); !has(got, "只要上衣", "两者都算") {
		t.Fatalf("品牌多值应压过单值（应出甲牌的两个），实际 %v", got)
	}

	if got := names(map[string]string{"categoryIds": ""}); has(got, "只要上衣", "只要裤子", "两者都算", "都不算") == false {
		t.Fatalf("空 categoryIds 应等价于不筛选，实际 %v", got)
	}
}
