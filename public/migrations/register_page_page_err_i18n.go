package migrations

import "sync"

func registerPagePageErrI18n() {
	registerPagePageErrI18nOnce.Do(registerPagePageErrI18nSeed)
}

var registerPagePageErrI18nOnce sync.Once

func registerPagePageErrI18nSeed() {

	registerSeed(Seed{
		Version:   "404-i18n-page-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'page.form.projectNameRequired','page.form.pathRequired')",
		SQL: mustSQL("404_i18n_page_page_err.sql"),
	})
}

func init() { registerPagePageErrI18n() }
