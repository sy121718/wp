package migrations

func init() {
	registerSeed(Seed{
		Version:   "446-i18n-product-category-tree",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.product_categories.children.empty'),
    ('admin.product_categories.children.pagePrefix'),
    ('admin.product_categories.children.pageSuffix'),
    ('admin.product_categories.children.paginationLabel'),
    ('admin.product_categories.search.matchCount'),
    ('admin.product_categories.search.matchSuffix'),
    ('catFindAncestor'),
    ('catFindMatched'),
    ('catParentEmpty'),
    ('catParentFind'),
    ('catParentFindPh')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("720g_current_446_i18n_product_category_tree.sql"),
	})
}
