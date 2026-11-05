package migrations

func init() {
	registerSeed(Seed{
		Version:   "737-product-pricing-preview-summary-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (('prPrevSummary', 'zh-CN'), ('prPrevSummary', 'en-US'))`,
		SQL: mustSQL("737_product_pricing_preview_summary_i18n.sql"),
	})
}
