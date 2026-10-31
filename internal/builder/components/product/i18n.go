package product

const (
	TextKeyStockNote  = "site.component.product.stockNote"
	TextKeyCategories = "site.component.product.categories"
	TextKeyReviews    = "site.component.product.reviews"
	TextKeyBrand      = "site.component.product.brand"
)

const (
	textFallbackStockNote  = "以结算时库存为准"
	textFallbackCategories = "商品分类"
	textFallbackReviews    = "条评价"
	textFallbackBrand      = "品牌"
)

type Labels struct {
	Categories string

	Reviews string

	Brand string
}

func defaultLabels() Labels {
	return Labels{
		Categories: textFallbackCategories,
		Reviews:    textFallbackReviews,
		Brand:      textFallbackBrand,
	}
}

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	v.LiveLabels = TranslateLiveLabels(text)
	base := defaultLabels()
	if text == nil {
		v.StockNote = textFallbackStockNote
		v.Labels = base
		return
	}
	v.StockNote = text(TextKeyStockNote, textFallbackStockNote)
	v.Labels = Labels{
		Categories: text(TextKeyCategories, base.Categories),
		Reviews:    text(TextKeyReviews, base.Reviews),
		Brand:      text(TextKeyBrand, base.Brand),
	}
}

type LiveLabels struct{ Delisted, Updated, Unknown, Out, Low, In string }

func TranslateLiveLabels(text func(string, string) string) LiveLabels {
	if text == nil {
		text = func(_, fallback string) string { return fallback }
	}
	return LiveLabels{
		Delisted: text("site.fragment.live_price.delisted", "该规格已下架，以结算为准"),
		Updated:  text("site.fragment.live_price.updated", "价格已更新为 %s，以结算为准"),
		Unknown:  text("site.fragment.stock.checkout", "以结算时库存为准"),
		Out:      text("site.fragment.stock.out", "暂时缺货"),
		Low:      text("site.fragment.stock.low", "仅剩 %d 件"),
		In:       text("site.fragment.stock.in", "库存充足"),
	}
}
