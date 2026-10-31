package migrations

import "sync"

func registerMailAdminPageErrI18n() {
	registerMailAdminPageErrI18nOnce.Do(registerMailAdminPageErrI18nSeed)
}

var registerMailAdminPageErrI18nOnce sync.Once

func registerMailAdminPageErrI18nSeed() {

	registerSeed(Seed{
		Version:   "410-i18n-mail-admin-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 5 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'mail.err.trackLinkInvalid','mail.err.unsubscribeLinkInvalid'," +
			"'mail.msg.unsubscribeDoneTitle','mail.msg.unsubscribeDoneBody'," +
			"'mail.msg.unsubscribeDoneNote')",
		SQL: mustSQL("410_i18n_mail_admin_page_err.sql"),
	})
}

func init() { registerMailAdminPageErrI18n() }
