package migrations

// register_product_media_gallery_i18n.go — 478：商品图集多图控件的文案。
//
// 注册方式：本文件自带 init()（与 477/476/475 同形）。
// 门槛判据枚举本批自己的 key（上界封闭）：不数全库、不按前缀匹配。
func init() {
	registerSeed(Seed{
		Version:   "478-product-media-gallery-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.products.media.add', 'admin.products.media.altPlaceholder', 'admin.products.media.empty')",
		SQL: mustSQL("478_product_media_gallery_i18n.sql"),
	})
}
