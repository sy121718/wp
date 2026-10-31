package migrations

import "sync"

func registerOrderPageErrI18n() {
	registerOrderPageErrI18nOnce.Do(registerOrderPageErrI18nSeed)
}

var registerOrderPageErrI18nOnce sync.Once

func registerOrderPageErrI18nSeed() {

	registerSeed(Seed{
		Version:   "406-i18n-order-page-err",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.common.list.loadFailedTitle','admin.common.list.loadFailedDesc')",
		SQL: mustSQL("406_i18n_order_page_err.sql"),
	})
}

func init() { registerOrderPageErrI18n() }
