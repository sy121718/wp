package migrations

import "sync"

func registerMiscPageErrI18n() {
	registerMiscPageErrI18nOnce.Do(registerMiscPageErrI18nSeed)
}

var registerMiscPageErrI18nOnce sync.Once

func registerMiscPageErrI18nSeed() {

	registerSeed(Seed{
		Version:   "408-i18n-misc-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.masterdata.loadFailed.lead','admin.masterdata.loadFailed.title'," +
			"'admin.masterdata.loadFailed.desc','admin.analytics.no_project.loadFailed'," +
			"'admin.content.templates.loadFailed.title','admin.content.templates.loadFailed'," +
			"'admin.content.templates.impact.loadFailed','ErrAnalyticsInternal')",
		SQL: mustSQL("408_i18n_misc_page_err.sql"),
	})
}

func init() { registerMiscPageErrI18n() }
