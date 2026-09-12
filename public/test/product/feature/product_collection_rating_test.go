// Package feature product 模块 feature 测试 —— 商品评分（issue #29）。
//
// 核心口径：**无评分（NULL）与 0 分严格区分**。
// 「评分 ≥ 4」的筛选里新上架商品不该因为「还没人评过」而被当成 0 分筛掉；
// 集合项也据此不给 ratingValue，让组件把它排在最后。
package feature

import (
	"context"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

// setRatings 给商品写若干条评分（issue #30：评分独立成表，走独立的评分接口 ——
// 商品更新接口不再有 rating 字段，评分不是商品的列）。
func setRatings(t *testing.T, f *detailFixture, productID string, scores ...float64) {
	t.Helper()
	for _, score := range scores {
		if _, err := f.products.AddRating(context.Background(), &productdto.AddRatingReq{
			ProductID: productID, Score: score,
		}); err != nil {
			t.Fatalf("写入评分失败: %v", err)
		}
	}
}

// TestProductCollectionRatingFilter 最低评分筛选（issue #29）。
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
	setRatings(t, f, high, 4.5, 5) // 平均 4.75
	setRatings(t, f, low, 3)
	_ = none // 不设评分：rating 保持 NULL

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

	// 评分 ≥ 4：只有高分货（低分 3 分不够、无评分的不入选）。
	if got := names(map[string]string{"minRating": "4"}); len(got) != 1 || !got["高分货"] {
		t.Fatalf("minRating=4 应只命中高分货，实际 %v", got)
	}
	// 评分 ≥ 3：两个有评分的都命中 —— **无评分的仍然不出现**。
	got := names(map[string]string{"minRating": "3"})
	if len(got) != 2 || !got["高分货"] || !got["低分货"] {
		t.Fatalf("minRating=3 应命中两个有评分的商品，实际 %v", got)
	}
	if got["无评分货"] {
		t.Fatalf("无评分商品不该出现在评分筛选结果里（NULL 不是 0 分）: %v", got)
	}
	// 评分 ≥ 0：仍然排除无评分的（0 是有效评分，但 NULL 不是）。
	if got := names(map[string]string{"minRating": "0"}); len(got) != 2 {
		t.Fatalf("minRating=0 仍应排除无评分商品，实际 %v", got)
	}
	// 与状态叠加：未发布商品不出现。
	if got := names(map[string]string{"status": "published", "minRating": "0"}); len(got) != 0 {
		t.Fatalf("未发布商品不该出现: %v", got)
	}

	// 非法输入：非数字 / 越界（>5、<0）。
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

// TestProductCollectionItemCarriesRating 集合项：有评分给数值 ratingValue + 展示字符串；
// 无评分时两个键都不给（组件据此排最后，而不是当 0 分比较）。
func TestProductCollectionItemCarriesRating(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)
	rated := f.createProduct(t, "已评分", "rated-one", "", 99, 99)
	f.createProduct(t, "未评分", "unrated-one", "", 99, 99)
	setRatings(t, f, rated, 4, 4.5) // 平均 4.25

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
