package migrations

func init() {
	registerSeed(Seed{
		Version:   "476-detail-preview-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.article.edit.previewRealHeading'),
    ('admin.article.edit.previewRealHint'),
    ('admin.article.edit.previewRefresh'),
    ('admin.article.preview.empty'),
    ('admin.article.preview.failed'),
    ('admin.article.preview.needSave'),
    ('admin.article.preview.unavailable'),
    ('admin.products.preview.empty'),
    ('admin.products.preview.failed'),
    ('admin.products.preview.unavailable'),
    ('pPrevHead'),
    ('pPrevHint'),
    ('pPrevNeedSave'),
    ('pPrevReload')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723h_current_476_detail_preview_i18n.sql"),
	})
}
