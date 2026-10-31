package migrations

func init() {
	registerSeed(Seed{
		Version:      "618-product-pricing-applied-i18n",
		TableName:    "sys_i18n",
		SQL:          mustSQL("618_product_pricing_applied_i18n.sql"),
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN ('admin.product_pricing.applied')",
	})
}
