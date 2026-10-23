package migrations

// register_translation_stale_mark_i18n.go — 迁移 620：译文保存后标记失败的回执词条。
//
// 与 service 的 StaleMarkFailed 配套：那一步失败时回执要换词，否则「已标记待重建」
// 会是一句假话（见 product_translations_page.go 的 TranslationsSavedMsg）。
func registerTranslationStaleMarkI18n() {
	register(Migration{
		Version:   "620-translation-stale-mark-i18n",
		TableName: "sys_i18n",
		// 判据是「这个 key 的这条 lang 已存在」，不是「表存在」——
		// 用表名判会让这条永远跳过（294 踩过的同一形态）。
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NOT NULL AND " +
			"EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'admin.product_translations.savedStaleMarkFailed' " +
			"AND lang = 'zh-CN') THEN 1 ELSE 0 END",
		SQL: mustSQL("620_translation_stale_mark_failed_i18n.sql"),
	})
}
