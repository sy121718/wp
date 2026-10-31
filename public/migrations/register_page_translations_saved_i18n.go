package migrations

import "sync"

func registerPageTranslationsSavedI18n() {
	registerPageTranslationsSavedI18nOnce.Do(registerPageTranslationsSavedI18nSeed)
}

var registerPageTranslationsSavedI18nOnce sync.Once

func registerPageTranslationsSavedI18nSeed() {

	registerSeed(Seed{
		Version:   "613-page-translations-saved-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.page_translations.saved', 'zh-CN'), ('admin.page_translations.saved', 'en-US'), " +
			"('admin.page_translations.saved_rebuild', 'zh-CN'), ('admin.page_translations.saved_rebuild', 'en-US'))",
		SQL: mustSQL("613_page_translations_saved_i18n.sql"),
	})
}
