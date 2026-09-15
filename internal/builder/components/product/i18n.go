package product

// i18n.go — 商品详情的固定文案取词（审计 I18N-010）。

// 文案 key（sys_i18n）：site.component.product.<语义>。
const TextKeyStockNote = "site.component.product.stockNote"

// textFallbackStockNote 无脚本时的库存兜底文案（中文原文，也是缺词条时的回退值）。
const textFallbackStockNote = "以结算时库存为准"

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
//
// 这句文案出现在**每个变体**的库存位：片段（productVariantAvailability）没接进来时
// 它就是访客唯一能看到的库存说明，所以它必须随语言变 —— 否则英文站点上
// 唯一一句“库存说明”是中文。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.StockNote = textFallbackStockNote
		return
	}
	v.StockNote = text(TextKeyStockNote, textFallbackStockNote)
}
