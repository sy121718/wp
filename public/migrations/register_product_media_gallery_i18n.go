package migrations

func init() {
	registerSeed(Seed{
		Version:   "478-product-media-gallery-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.products.media.add', 'admin.products.media.altPlaceholder', 'admin.products.media.empty')",
		SQL: mustSQL("478_product_media_gallery_i18n.sql"),
	})
}
