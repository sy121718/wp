package migrations

func init() {
	registerSeed(Seed{
		Version:   "468-product-comment-policy-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +

			"'product.err.commentPurchaseRequired'" +
			")",
		SQL: mustSQL("468_product_comment_policy_i18n.sql"),
	})
}
