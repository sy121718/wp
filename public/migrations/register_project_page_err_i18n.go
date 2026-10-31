package migrations

import "sync"

func registerProjectPageErrI18n() {
	registerProjectPageErrI18nOnce.Do(registerProjectPageErrI18nSeed)
}

var registerProjectPageErrI18nOnce sync.Once

func registerProjectPageErrI18nSeed() {

	registerSeed(Seed{
		Version:   "403-i18n-project-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 9 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'ErrProjectRequired','ErrThemeIDRequired','MsgThemeSettingsInvalid','MsgThemeSettingsRefreshFailed'," +
			"'ErrSiteSettingsNameRequired','ErrGA4IDInvalid','ErrGSCVerificationInvalid'," +
			"'ErrNotFoundHTMLTooLong','ErrLangURLModeInvalid')",
		SQL: mustSQL("403_i18n_project_page_err.sql"),
	})
}

func init() { registerProjectPageErrI18n() }
