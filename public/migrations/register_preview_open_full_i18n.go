package migrations

func init() {
	registerSeed(Seed{
		Version:   "477-preview-open-full-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.article.edit.previewOpenFull', 'admin.products.preview.openFull')",
		SQL: mustSQL("477_preview_open_full_i18n.sql"),
	})
}
