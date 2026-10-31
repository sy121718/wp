package migrations

func init() {
	registerSeed(Seed{
		Version:   "476-detail-preview-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 28 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.article.edit.previewRealHeading', 'admin.article.edit.previewRealHint', 'admin.article.edit.previewRefresh', 'admin.article.preview.needSave', 'admin.article.preview.unavailable', 'admin.article.preview.failed', 'admin.article.preview.empty', 'admin.products.preview.heading', 'admin.products.preview.hint', 'admin.products.preview.refresh', 'admin.products.preview.needSave', 'admin.products.preview.unavailable', 'admin.products.preview.failed', 'admin.products.preview.empty')",
		SQL: mustSQL("476_detail_preview_i18n.sql"),
	})
}
