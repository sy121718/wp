package migrations

import "sync"

// register_page_translation_misses_entry_i18n.go — 页面列表页「缺译报告」入口词条（495）。
func registerPageTranslationMissesEntryI18n() {
	registerPageTranslationMissesEntryI18nOnce.Do(registerPageTranslationMissesEntryI18nSeed)
}

// registerPageTranslationMissesEntryI18nOnce 让重复调用成为空操作。
var registerPageTranslationMissesEntryI18nOnce sync.Once

// registerPageTranslationMissesEntryI18nSeed 注册 495（真正干活的那一半）。
func registerPageTranslationMissesEntryI18nSeed() {
	// 495：1 个 key × 2 语言。门槛判据按本批自己的 key 逐条枚举（写进 SQL 字面量）。
	registerSeed(Seed{
		Version:   "495-i18n-page-translation-misses-entry",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.pages.action.translation_misses' AND lang IN ('zh-CN','en-US')",
		SQL: mustSQL("495_page_translation_misses_entry_i18n.sql"),
	})
}
