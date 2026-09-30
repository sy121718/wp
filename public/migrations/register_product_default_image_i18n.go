package migrations

// register_product_default_image_i18n.go — 479：商品主图字段的文案。
//
// 注册方式：本文件自带 init()（与 478/477/476 同形）。
// 门槛判据枚举本批自己的 key（上界封闭）：不数全库、不按前缀匹配。
func init() {
	registerSeed(Seed{
		Version:   "479-product-default-image-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.products.label.defaultImage')",
		SQL: mustSQL("479_product_default_image_i18n.sql"),
	})
}
