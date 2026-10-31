package migrations

func init() {
	registerSeed(Seed{
		Version:   "479-product-default-image-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.products.label.defaultImage')",
		SQL: mustSQL("479_product_default_image_i18n.sql"),
	})
}
