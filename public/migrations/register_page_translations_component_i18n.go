package migrations

import "sync"

func registerPageTranslationsComponentI18n() {
	registerPageTranslationsComponentI18nOnce.Do(registerPageTranslationsComponentI18nSeed)
}

var registerPageTranslationsComponentI18nOnce sync.Once

func registerPageTranslationsComponentI18nSeed() {

	registerSeed(Seed{
		Version:   "428-i18n-page-translations-component-col",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.page_translations.col.component')",
		SQL: mustSQL("428_i18n_page_translations_component_col.sql"),
	})
}

func init() { registerPageTranslationsComponentI18n() }
