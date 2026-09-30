package migrations

// register_detail_preview_i18n.go — 476：详情页真实预览的界面文案。
//
// 注册方式：本文件自带 init()（与 475/473/471 同形）。
// 门槛判据枚举本批自己的 key（上界封闭）：不数全库、不按前缀匹配。
func init() {
	registerSeed(Seed{
		Version:   "476-detail-preview-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 28 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.article.edit.previewRealHeading', 'admin.article.edit.previewRealHint', 'admin.article.edit.previewRefresh', 'admin.article.preview.needSave', 'admin.article.preview.unavailable', 'admin.article.preview.failed', 'admin.article.preview.empty', 'admin.products.preview.heading', 'admin.products.preview.hint', 'admin.products.preview.refresh', 'admin.products.preview.needSave', 'admin.products.preview.unavailable', 'admin.products.preview.failed', 'admin.products.preview.empty')",
		SQL: mustSQL("476_detail_preview_i18n.sql"),
	})
}
