package productservice

// product_row_facts_unit_test.go — 行装配原料的**纯函数**判据（不起库、不 import support）。
//
// 为什么这一份留在内部测试包：它要直接调 applyRowFacts / ratingRespOfAgg（未导出）。
// 带库的判据在外部测试包 productservice_test 里 —— support 经 internal/routers 传递依赖
// product/service，内部测试包导入它会构成测试期导入环（go vet: import cycle not allowed in test）。
//
// applyRowFacts 不碰价格的理由：列表项的价格区间由变体派生（applyPriceRange），
// 行原料只补挂载 id —— 它若顺手改写价格，列表页与详情页就会各说一套（不报错、测试也不红）。

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
)

// TestApplyRowFactsKeepsPrice 行原料只补挂载 id，不改写价格区间。
func TestApplyRowFactsKeepsPrice(t *testing.T) {
	item := &productdto.ProductResp{PriceMin: 1.50, PriceMax: 9.50}
	applyRowFacts(item, productmodel.ProductRowFacts{})
	if item.PriceMin != 1.50 || item.PriceMax != 9.50 {
		t.Fatalf("行原料不该改写价格区间：%v~%v", item.PriceMin, item.PriceMax)
	}
	if item.DefaultPrice != nil {
		t.Fatalf("行原料不该填商品级默认售价：%v", item.DefaultPrice)
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
