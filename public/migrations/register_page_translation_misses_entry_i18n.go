package migrations

import "sync"

func registerPageTranslationMissesEntryI18n() {
	registerPageTranslationMissesEntryI18nOnce.Do(registerPageTranslationMissesEntryI18nSeed)
}

var registerPageTranslationMissesEntryI18nOnce sync.Once

func registerPageTranslationMissesEntryI18nSeed() {

	registerSeed(Seed{
		Version:   "495-i18n-page-translation-misses-entry",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.pages.action.translation_misses' AND lang IN ('zh-CN','en-US')",
		SQL: mustSQL("495_page_translation_misses_entry_i18n.sql"),
	})
}
