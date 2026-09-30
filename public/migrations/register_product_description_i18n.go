package migrations

// register_product_description_i18n.go — 481：商品描述字段的标签文案。
//
// 注册方式：本文件自带 init()（与 480/479/478 同形）。
// 门槛判据枚举本批自己的 key（上界封闭）：不数全库、不按前缀匹配。
func init() {
	registerSeed(Seed{
		Version:   "481-product-description-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.products.label.description'",
		SQL: mustSQL("481_product_description_i18n.sql"),
	})
}
