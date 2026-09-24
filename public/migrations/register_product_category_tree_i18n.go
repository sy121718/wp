package migrations

func init() {
	registerSeed(Seed{
		Version:   "446-i18n-product-category-tree",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 22 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.product_categories.search.matchCount', 'admin.product_categories.search.matchSuffix', " +
			"'admin.product_categories.search.matched', 'admin.product_categories.search.ancestor', " +
			"'admin.product_categories.children.empty', 'admin.product_categories.children.paginationLabel', " +
			"'admin.product_categories.children.pagePrefix', 'admin.product_categories.children.pageSuffix', " +
			"'admin.product_categories.parentSearch.label', 'admin.product_categories.parentSearch.placeholder', " +
			"'admin.product_categories.parentSearch.empty') AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("446_i18n_product_category_tree.sql"),
	})
}
