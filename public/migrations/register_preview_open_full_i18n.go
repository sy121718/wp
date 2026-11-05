package migrations

func init() {
	registerSeed(Seed{
		Version:   "477-preview-open-full-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.article.edit.previewOpenFull'),
    ('pPrevOpenFull')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723q_current_477_preview_open_full_i18n.sql"),
	})
}
