package migrations

// register_product_gallery_single_entry_i18n.go — 483：图集空态文案改口（添加入口收成「＋」框）。
//
// 注册方式：本文件自带 init()（与 482/481/480 同形）。
// 门槛判据枚举本批自己的 key（上界封闭）：不数全库、不按前缀匹配。
// 判据写「值已不是 478 的原值」—— 运维改过的文案也算已应用，避免每次启动把它改回去。
func init() {
	registerSeed(Seed{
		Version:   "483-product-gallery-single-entry-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE ` +
			`(item_key = 'admin.products.media.empty' AND lang = 'zh-CN' ` +
			`AND item_value <> '还没有图片。点「添加图片」从媒体库里挑 —— 上传也在媒体库里做。') OR ` +
			`(item_key = 'admin.products.media.empty' AND lang = 'en-US' ` +
			`AND item_value <> 'No images yet. Use "Add images" to pick from the media library — uploads happen there too.')`,
		SQL: mustSQL("483_product_gallery_single_entry_i18n.sql"),
	})
}
