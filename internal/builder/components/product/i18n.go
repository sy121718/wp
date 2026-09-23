package product

// i18n.go — 商品详情的固定文案取词（审计 I18N-010）。

// 文案 key（sys_i18n）：site.component.product.<语义>。
const (
	TextKeyStockNote  = "site.component.product.stockNote"
	TextKeyCategories = "site.component.product.categories"
	TextKeyReviews    = "site.component.product.reviews"
	TextKeyBrand      = "site.component.product.brand"
)

// 兜底文案（中文原文，也是缺词条时的回退值）。
const (
	textFallbackStockNote  = "以结算时库存为准"
	textFallbackCategories = "商品分类"
	textFallbackReviews    = "条评价"
	textFallbackBrand      = "品牌"
)

// Labels 商品详情里的固定文案（多语言 P4）。
type Labels struct {
	// Categories 分类链接区的无障碍标签（视觉上不显示，读屏用）。
	Categories string
	// Reviews 评价数后缀（"12 条评价" 里的 "条评价"）。
	Reviews string
	// Brand 品牌行的标签（"品牌：xxx" 里的 "品牌"）。
	Brand string
}

// defaultLabels 全部兜底文案。
func defaultLabels() Labels {
	return Labels{
		Categories: textFallbackCategories,
		Reviews:    textFallbackReviews,
		Brand:      textFallbackBrand,
	}
}

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
//
// 这句文案出现在**每个变体**的库存位：片段（productVariantAvailability）没接进来时
// 它就是访客唯一能看到的库存说明，所以它必须随语言变 —— 否则英文站点上
// 唯一一句“库存说明”是中文。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
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
