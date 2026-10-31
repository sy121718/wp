package migrations

import "sync"

func registerLocaleAdmissionI18n() {
	registerLocaleAdmissionI18nOnce.Do(registerLocaleAdmissionI18nSeed)
}

var registerLocaleAdmissionI18nOnce sync.Once

func registerLocaleAdmissionI18nSeed() {

	registerSeed(Seed{
		Version:   "492-i18n-locale-admission-miss-report",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 13 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'ErrLocaleNoTranslations'," +
			"'admin.page.translation_misses.title'," +
			"'admin.page.translation_misses.back'," +
			"'admin.page.translation_misses.hint'," +
			"'admin.page.translation_misses.col.page'," +
			"'admin.page.translation_misses.col.lang'," +
			"'admin.page.translation_misses.col.misses'," +
			"'admin.page.translation_misses.col.actions'," +
			"'admin.page.translation_misses.empty'," +
			"'admin.page.translation_misses.unit.misses'," +
			"'admin.page.translation_misses.candidates'," +
			"'admin.page.translation_misses.action.cancel'," +
			"'admin.page.translation_misses.canceled'" +
			")",
		SQL: mustSQL("492_locale_admission_and_miss_report_i18n.sql"),
	})
}
