package migrations

func init() {
	registerSeed(Seed{
		Version:   "481-product-description-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.products.label.description'",
		SQL: mustSQL("481_product_description_i18n.sql"),
	})
}
