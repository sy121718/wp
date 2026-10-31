package productservice

import (
	"reflect"
	"testing"

	productmodel "go_wp/internal/module/product/model"
)

func TestListProjectionKeepsPriceAndReferences(t *testing.T) {
	primary, brand := "category-a", "brand-a"
	row := &productmodel.ProductEntity{
		AttributeIDs: []byte(`["attribute-a"]`), CategoryIDs: []byte(`["category-a"]`),
		TagIDs: []byte(`["tag-a"]`), ImageAlts: []byte(`["主图"]`),
		PrimaryCategoryID: &primary, BrandID: &brand,
	}
	item := (&Service{}).toListResp(row)
	if !reflect.DeepEqual(item.AttributeIDs, []string{"attribute-a"}) || !reflect.DeepEqual(item.CategoryIDs, []string{primary}) || !reflect.DeepEqual(item.TagIDs, []string{"tag-a"}) || !reflect.DeepEqual(item.ImageAlts, []string{"主图"}) || item.PrimaryCategoryID != primary || item.BrandID != brand {
		t.Fatalf("列表投影丢失挂载字段：%+v", item)
	}
	applyPriceRange(item, []*productmodel.VariantEntity{{Price: 1.50}, {Price: 9.50}})
	if item.PriceMin != 1.50 || item.PriceMax != 9.50 || item.DefaultPrice != nil {
		t.Fatalf("挂载投影干扰变体价格：%+v", item)
	}
	if item.PrimaryCategoryID != primary || item.BrandID != brand {
		t.Fatal("价格派生改写挂载事实")
	}
}

func TestRatingRespOfAggZeroValue(t *testing.T) {
	zero := ratingRespOfAgg(productmodel.RatingAgg{})
	if zero == nil {
		t.Fatal("零值聚合也要给出非 nil 的评分投影（行装配按 nil 兜底会掩盖调用方传错）")
	}
	if zero.HasRating || zero.RatingCount != 0 || zero.Rating != 0 {
		t.Fatalf("无评分商品的投影应为零值：%+v", *zero)
	}
	if len(zero.Items) != 0 {
		t.Fatalf("批量路径不加载评分明细，实际 %d 条", len(zero.Items))
	}

	one := ratingRespOfAgg(productmodel.RatingAgg{Avg: 4.25, Count: 3})
	if !one.HasRating || one.RatingCount != 3 || one.Rating != 4.25 {
		t.Fatalf("有评分商品的投影不对：%+v", *one)
	}
}
