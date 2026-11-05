package migrations

func init() {
	registerSeed(Seed{
		Version:   "725-page-translation-receipt-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (
  SELECT 1 FROM (VALUES ('pageTrSaved'), ('pageTrRebuild')) AS keys(item_key)
  CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
  WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)
) AND NOT EXISTS (SELECT 1 FROM sys_i18n WHERE item_key IN ('admin.page_translations.saved', 'admin.page_translations.saved_rebuild'))
THEN 1 ELSE 0 END`,
		SQL: mustSQL("725_page_translation_receipt_i18n.sql"),
	})
}
