package productservice

// product_variant_price_test.go — 价格派生纯函数的判据（PRD-010 / DEC-01 已裁决）。
//
// 口径（DEC-01）：公开价格只看**启用**变体；划线价与售价取**同一个**胜出变体，
// 不跨变体取最大划线价（否则「最低价 10 元 + 另一个变体 99 元划线」会拼出假折扣）。
// 这里钉住机械规则：最低/最高价、同价决胜（id 小者胜）、划线价归属、空输入。

import (
	"testing"

	productmodel "go_wp/internal/module/product/model"
)

func priceVariant(id string, price float64, enabled bool, compare *float64) *productmodel.VariantEntity {
	return &productmodel.VariantEntity{ID: id, Price: price, Enabled: enabled, ComparePrice: compare}
}

func floatPtr(v float64) *float64 { return &v }

func TestSummarizeVariantPrices(t *testing.T) {
	t.Run("空输入给零值", func(t *testing.T) {
		summary := summarizeVariantPrices(nil, true)
		if summary.HasAny || summary.MinVariant != nil || summary.ComparePrice() != nil {
			t.Fatalf("空输入应给零值，实际 %+v", summary)
		}
	})

	t.Run("只算启用变体时不看禁用价", func(t *testing.T) {
		variants := []*productmodel.VariantEntity{
			priceVariant("b", 30, true, nil),
			priceVariant("a", 10, false, nil),
			priceVariant("c", 50, true, nil),
		}
		summary := summarizeVariantPrices(variants, true)
		if !summary.HasAny || summary.MinPrice != 30 || summary.MaxPrice != 50 {
			t.Fatalf("启用范围汇总错误：%+v", summary)
		}
		if summary.MinVariant == nil || summary.MinVariant.ID != "b" {
			t.Fatalf("最低价变体应是 b，实际 %+v", summary.MinVariant)
		}
	})

	t.Run("全部变体都算时禁用价进区间", func(t *testing.T) {
		variants := []*productmodel.VariantEntity{
			priceVariant("b", 30, true, nil),
			priceVariant("a", 10, false, nil),
		}
		summary := summarizeVariantPrices(variants, false)
		if summary.MinPrice != 10 || summary.MaxPrice != 30 || summary.MinVariant.ID != "a" {
			t.Fatalf("全量范围汇总错误：%+v", summary)
		}
	})

	t.Run("同价按 id 小者胜并且顺序无关", func(t *testing.T) {
		forward := summarizeVariantPrices([]*productmodel.VariantEntity{
			priceVariant("zzz", 20, true, nil),
			priceVariant("aaa", 20, true, nil),
		}, true)
		reverse := summarizeVariantPrices([]*productmodel.VariantEntity{
			priceVariant("aaa", 20, true, nil),
			priceVariant("zzz", 20, true, nil),
		}, true)
		if forward.MinVariant.ID != "aaa" || reverse.MinVariant.ID != "aaa" {
			t.Fatalf("同价决胜应稳定取 id 小者：forward=%s reverse=%s", forward.MinVariant.ID, reverse.MinVariant.ID)
		}
	})

	t.Run("划线价取胜出变体自己的", func(t *testing.T) {
		variants := []*productmodel.VariantEntity{
			priceVariant("a", 10, true, floatPtr(99)),
			priceVariant("b", 20, true, floatPtr(120)),
		}
		summary := summarizeVariantPrices(variants, true)
		if summary.MinVariant == nil || summary.MinVariant.ID != "a" {
			t.Fatalf("最低价变体应是 a，实际 %+v", summary.MinVariant)
		}
		compare := summary.ComparePrice()
		if compare == nil || *compare != 99 {
			t.Fatalf("划线价应取最低价变体自己的 99，实际 %v", compare)
		}
	})

	t.Run("最高价变体的划线价不冒充最低价折扣", func(t *testing.T) {
		variants := []*productmodel.VariantEntity{
			priceVariant("a", 10, true, nil),
			priceVariant("b", 20, true, floatPtr(999)),
		}
		summary := summarizeVariantPrices(variants, true)
		if summary.ComparePrice() != nil {
			t.Fatalf("最低价变体没有划线价时不应借用别的变体，实际 %v", *summary.ComparePrice())
		}
	})

	t.Run("没有划线价时不产出 compare", func(t *testing.T) {
		summary := summarizeVariantPrices([]*productmodel.VariantEntity{priceVariant("a", 10, true, nil)}, true)
		if summary.ComparePrice() != nil {
			t.Fatalf("无划线价不应产出 compare，实际 %v", *summary.ComparePrice())
		}
	})

	t.Run("切片里混入 nil 不 panic", func(t *testing.T) {
		summary := summarizeVariantPrices([]*productmodel.VariantEntity{nil, priceVariant("a", 10, true, nil)}, true)
		if summary.MinVariant == nil || summary.MinVariant.ID != "a" {
			t.Fatalf("nil 变体应被跳过，实际 %+v", summary.MinVariant)
		}
	})

	t.Run("没有启用变体时 HasAny 为假", func(t *testing.T) {
		summary := summarizeVariantPrices([]*productmodel.VariantEntity{priceVariant("a", 10, false, nil)}, true)
		if summary.HasAny || summary.MinVariant != nil {
			t.Fatalf("全部停用时不应产出最低价，实际 %+v", summary)
		}
	})
}
