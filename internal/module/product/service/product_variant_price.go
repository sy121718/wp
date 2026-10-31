package productservice

// product_variant_price.go — 变体价格的派生口径（纯函数，PRD-010）。
//
// 为什么单开一个文件而不是各写一遍：价格汇总在两条**公开**读取路径上各有一份——
// 目录（product_catalog.go）与集合源字段值（product_source.go）。两份的「启用范围」
// 与「划线价取谁的」并不相同（DEC-01 待定，本批**不改**口径），但它们共享同一套
// 机械规则：最低价/最高价、同价时谁胜、没有可用变体时怎么办。
//
// 各写一遍的后果不是「代码难看」，而是**同价决胜悄悄分叉**：目录按 (价, ID) 决胜，
// 集合源只看 `<`，于是同一个商品在商品卡与目录里给出不同的划线价，页面上两个数字
// 对不上，而两条路径各自的测试都是绿的。这里把决胜规则收成唯一实现。

import productmodel "go_wp/internal/module/product/model"

// variantPriceSummary 一组变体在**给定启用范围**下的价格事实。
//
// MinVariant 是「胜出的最低价变体」。公开价格与划线价都读它身上的事实（DEC-01 裁决）：
// 划线价必须与售价来自**同一个变体**，不能拿「最低价」配「另一个变体的最高划线价」——
// 那会把「10 元卖、别人 99 元划线」拼成 89% 折扣，页面上是假的。
type variantPriceSummary struct {
	HasAny     bool
	MinPrice   float64
	MaxPrice   float64
	MinVariant *productmodel.VariantEntity
}

// summarizeVariantPrices 按启用范围汇总价格；onlyEnabled=false 表示「全部变体都算」。
//
// 同价决胜规则：**id 更小者胜**（字符串比较）—— 与目录原有实现逐字相同。
// 这条规则不是审美：变体列表有确定性排序（sort ASC），但价格相同且 sort 也相同时
// 只按 `<` 比较会保留「先遍历到的那个」，而遍历顺序取决于 SQL 返回顺序（表连接方式
// 一变就变），于是同一份数据在两次构建里可能给出不同划线价 —— 违反不变量 5（确定性构建）。
//
// 空指针变体（切片里混入 nil）跳过：调用方来自 model 查询，不会出现，但这是纯函数，
// 不把「上游一定不给 nil」当契约，否则一次 Preload 变更就会在这里 panic。
func summarizeVariantPrices(variants []*productmodel.VariantEntity, onlyEnabled bool) variantPriceSummary {
	var out variantPriceSummary
	for _, v := range variants {
		if v == nil {
			continue
		}
		if onlyEnabled && !v.Enabled {
			continue
		}
		if !out.HasAny {
			out.HasAny = true
			out.MinPrice, out.MaxPrice = v.Price, v.Price
			out.MinVariant = v
		} else {
			if v.Price > out.MaxPrice {
				out.MaxPrice = v.Price
			}
			if v.Price < out.MinPrice || (v.Price == out.MinPrice && cheaperVariantWins(v, out.MinVariant)) {
				out.MinPrice, out.MinVariant = v.Price, v
			}
		}
	}
	return out
}

// ComparePrice 胜出最低价变体的划线价（没有就是 nil）。
//
// 单列方法而不是让调用方再读 MinVariant.ComparePrice：调用点会退化成
// 「先判 MinVariant 非空、再判字段非空」的三段式，多一处就多一次漏判的机会。
func (s variantPriceSummary) ComparePrice() *float64 {
	if s.MinVariant == nil {
		return nil
	}
	return s.MinVariant.ComparePrice
}

// cheaperVariantWins 同价时是否由 challenger 胜出（id 更小者胜；id 相同则保持先到者）。
func cheaperVariantWins(challenger, current *productmodel.VariantEntity) bool {
	if current == nil {
		return true
	}
	return challenger.ID < current.ID
}
