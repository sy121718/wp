package migrations

func init() {
	registerSeed(Seed{
		Version:   "696-product-mvc-facts-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 14 THEN 1 ELSE 0 END FROM sys_i18n
			WHERE lang IN ('zh-CN','en-US') AND item_key IN (
			'admin.seo.coverage.incomplete',
			'admin.seo.profile.landing',
			'admin.seo.profile.guide',
			'product.err.detailTemplateNameRequired',
			'product.err.detailTemplatePathRequired',
			'product.err.detailTemplateUnavailable',
			'product.err.detailTemplatePreviewEmpty')`,
		SQL: mustSQL("696_product_mvc_facts_i18n.sql"),
	})
}
