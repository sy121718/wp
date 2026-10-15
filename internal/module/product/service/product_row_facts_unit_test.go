package productservice

// product_row_facts_unit_test.go — 行装配原料的**纯函数**判据（不起库、不 import support）。
//
// 为什么这一份留在内部测试包：它要直接调 applyRowFacts / ratingRespOfAgg（未导出）。
// 带库的判据在外部测试包 productservice_test 里 —— support 经 internal/routers 传递依赖
// product/service，内部测试包导入它会构成测试期导入环（go vet: import cycle not allowed in test）。
//
// 容器价单列一条的理由：它是列表路径**唯一**「按另一处规则补算」的字段 ——
// applyRowFacts 对 applyPriceRange 的 bundle 分支。两处各写一遍就会分叉，
// 而分叉的表现只是列表页上的价格与详情页不一样（不报错、测试也不会红）。

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
)

// TestApplyRowFactsBundlePriceMatchesApplyPriceRange 容器价两处口径同值。
func TestApplyRowFactsBundlePriceMatchesApplyPriceRange(t *testing.T) {
	price := 88.50

	listSide := &productdto.ProductResp{Type: productmodel.TypeBundle}
	applyRowFacts(listSide, productmodel.ProductRowFacts{Type: productmodel.TypeBundle, DefaultPrice: &price})

	detailSide := &productdto.ProductResp{Type: productmodel.TypeBundle, DefaultPrice: &price}
	applyPriceRange(detailSide, nil)

	if listSide.PriceMin != detailSide.PriceMin || listSide.PriceMax != detailSide.PriceMax {
		t.Fatalf("容器价两处口径不一致：列表路径 %v~%v、详情路径 %v~%v",
			listSide.PriceMin, listSide.PriceMax, detailSide.PriceMin, detailSide.PriceMax)
	}
	if listSide.PriceMin != price || listSide.PriceMax != price {
		t.Fatalf("容器商品的价格应等于容器价 %.2f，实际 %v~%v", price, listSide.PriceMin, listSide.PriceMax)
	}
	if listSide.DefaultPrice == nil || *listSide.DefaultPrice != price {
		t.Fatalf("容器价本身也要落到行原料上，实际 %v", listSide.DefaultPrice)
	}

	// 容器价为空：两处都不改写（保持零值，模板显示 —）。
	empty := &productdto.ProductResp{Type: productmodel.TypeBundle}
	applyRowFacts(empty, productmodel.ProductRowFacts{Type: productmodel.TypeBundle})
	emptyDetail := &productdto.ProductResp{Type: productmodel.TypeBundle}
	applyPriceRange(emptyDetail, nil)
	if empty.PriceMin != emptyDetail.PriceMin || empty.PriceMax != emptyDetail.PriceMax {
		t.Fatalf("容器价为空时两处不一致：列表路径 %v~%v、详情路径 %v~%v",
			empty.PriceMin, empty.PriceMax, emptyDetail.PriceMin, emptyDetail.PriceMax)
	}

	// 变体商品：列表项的价格来自变体派生，**不该**被 default_price 改写。
	variant := &productdto.ProductResp{Type: productmodel.TypeVariant, PriceMin: 1.50, PriceMax: 9.50}
	applyRowFacts(variant, productmodel.ProductRowFacts{Type: productmodel.TypeVariant, DefaultPrice: &price})
	if variant.PriceMin != 1.50 || variant.PriceMax != 9.50 {
		t.Fatalf("变体商品的列表价不该被容器价改写：%v~%v", variant.PriceMin, variant.PriceMax)
	}
}

// TestRatingRespOfAggZeroValue 无评分商品的零值投影：不在 map ⇒ Count=0 ⇒ HasRating=false。
//
// 这条对应 dd.txt 3.5 的「无评分商品的零值表示」：与详情路径「确实没有评分」时的渲染一致
// （HasRating=false、RatingCount=0、明细为空）。**读不到不走这条路**：两条路径都不吞错 ——
// 批量侧是 productRowsFromList 返回 error，详情 / 编辑侧是 ratingsOf 返回 error
// （service/product_row_rating_failure_test.go 用失败注入钉住这两档的区别）。
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
