package migrations

func init() {
	registerSeed(Seed{
		Version:   "438-i18n-product-rating-count-header",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.products.rating.countHeader' AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("438_i18n_product_rating_count_header.sql"),
	})
}
