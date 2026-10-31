package feature

import (
	"context"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

func setRatings(t *testing.T, f *detailFixture, productID string, scores ...float64) {
	t.Helper()
	for _, score := range scores {
		if _, err := f.products.AddRating(context.Background(), &productdto.AddRatingReq{
			ProductID: productID, Score: &score,
		}); err != nil {
			t.Fatalf("写入评分失败: %v", err)
		}
	}
}

func TestProductCollectionRatingFilter(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)

	high := f.createProduct(t, "高分货", "high-rated", "", 99, 99)
	low := f.createProduct(t, "低分货", "low-rated", "", 99, 99)
	none := f.createProduct(t, "无评分货", "no-rating", "", 99, 99)
	setRatings(t, f, high, 4.5, 5)
	setRatings(t, f, low, 3)
	_ = none

	names := func(filter map[string]string) map[string]bool {
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

	if got := names(map[string]string{"minRating": "4"}); len(got) != 1 || !got["高分货"] {
		t.Fatalf("minRating=4 应只命中高分货，实际 %v", got)
	}

	got := names(map[string]string{"minRating": "3"})
	if len(got) != 2 || !got["高分货"] || !got["低分货"] {
		t.Fatalf("minRating=3 应命中两个有评分的商品，实际 %v", got)
	}
	if got["无评分货"] {
		t.Fatalf("无评分商品不该出现在评分筛选结果里（NULL 不是 0 分）: %v", got)
	}

	if got := names(map[string]string{"minRating": "0"}); len(got) != 2 {
		t.Fatalf("minRating=0 仍应排除无评分商品，实际 %v", got)
	}

	if got := names(map[string]string{"status": "published", "minRating": "0"}); len(got) != 0 {
		t.Fatalf("未发布商品不该出现: %v", got)
	}

	for _, filter := range []map[string]string{
		{"minRating": "abc"},
		{"minRating": "6"},
		{"minRating": "-1"},
	} {
		if _, rerr := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, filter); rerr == nil {
			t.Fatalf("非法评分筛选 %v 应被拒绝", filter)
		}
	}
}

func TestProductCollectionItemCarriesRating(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)
	rated := f.createProduct(t, "已评分", "rated-one", "", 99, 99)
	f.createProduct(t, "未评分", "unrated-one", "", 99, 99)
	setRatings(t, f, rated, 4, 4.5)

	items, err := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, map[string]string{})
	if err != nil {
		t.Fatalf("解析集合失败: %v", err)
	}
	seen := map[string]map[string]any{}
	for _, item := range items {
		if name, ok := item["name"].(string); ok {
			seen[name] = item
		}
	}
	ratedItem, ok := seen["已评分"]
	if !ok {
		t.Fatalf("缺已评分商品: %v", seen)
	}
	if v, ok := ratedItem["ratingValue"].(float64); !ok || v != 4.25 {
		t.Fatalf("已评分商品应带数值 ratingValue: %#v", ratedItem["ratingValue"])
	}
	if s, _ := ratedItem["rating"].(string); s != "4.25" {
		t.Fatalf("rating 展示值应为两位小数: %#v", ratedItem["rating"])
	}
	unrated, ok := seen["未评分"]
	if !ok {
		t.Fatalf("缺未评分商品: %v", seen)
	}
	if _, exists := unrated["ratingValue"]; exists {
		t.Fatalf("无评分商品不该带 ratingValue（NULL 不是 0 分）: %#v", unrated["ratingValue"])
	}
}
