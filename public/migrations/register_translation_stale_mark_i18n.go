package migrations

func registerTranslationStaleMarkI18n() {
	register(Migration{
		Version:   "620-translation-stale-mark-i18n",
		TableName: "sys_i18n",

		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NOT NULL AND " +
			"EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.product_translations.savedStaleMarkFailed' " +
			"AND lang = 'zh-CN') THEN 1 ELSE 0 END",
		SQL: mustSQL("620_translation_stale_mark_failed_i18n.sql"),
	})
}
