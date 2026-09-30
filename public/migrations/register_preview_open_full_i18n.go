package migrations

// register_preview_open_full_i18n.go — 477：详情页预览的「1:1 打开」文案。
//
// 注册方式：本文件自带 init()（与 476/475/473 同形）。
// 门槛判据枚举本批自己的 key（上界封闭）：不数全库、不按前缀匹配。
func init() {
	registerSeed(Seed{
		Version:   "477-preview-open-full-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.article.edit.previewOpenFull', 'admin.products.preview.openFull')",
		SQL: mustSQL("477_preview_open_full_i18n.sql"),
	})
}
