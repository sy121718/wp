package migrations

func init() {
	registerSeed(Seed{
		Version:   "440-i18n-product-list-help",
		TableName: "sys_i18n",
		// A custom value also counts as settled; only the two original 233 tail values need rewriting.
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			"(item_key = 'admin.products.hint.detailTail' AND lang = 'zh-CN' AND item_value <> '里维护（点行的「详情」进入）。评分是独立明细：平均值与条数由明细算出，改评分不改动商品字段；「没有评分」与「评分 0 分」是两回事。') OR " +
			"(item_key = 'admin.products.hint.detailTail' AND lang = 'en-US' AND item_value <> 'page (open it via Detail in the row). Ratings live in their own detail table: the average and count are derived from it, and editing a rating never touches the product''s own fields. No ratings and rated 0 are different things.')",
		SQL: mustSQL("440_i18n_product_list_help.sql"),
	})
}
