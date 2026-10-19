package migrations

// 618 — 应用调价的回执合并成一条整句（旧 lead / tail 两条同批删除）。
//
// 门槛判据逐条枚举本批自己的 item_key（上界封闭），行数 = 1 key x 2 语言 = 2。
func init() {
	registerSeed(Seed{
		Version:      "618-product-pricing-applied-i18n",
		TableName:    "sys_i18n",
		SQL:          mustSQL("618_product_pricing_applied_i18n.sql"),
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN ('admin.product_pricing.applied')",
	})
}
