package migrations

import "sync"

func registerCustomersStatusHelpI18n() {
	registerCustomersStatusHelpI18nOnce.Do(registerCustomersStatusHelpI18nSeed)
}

var registerCustomersStatusHelpI18nOnce sync.Once

func registerCustomersStatusHelpI18nSeed() {

	registerSeed(Seed{
		Version:   "431-i18n-customers-status-help",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 5 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.customers.status.help.pending','admin.customers.status.help.locked'," +
			"'admin.customers.status.help.failures'," +
			"'admin.customers.list.empty_filtered_heading','admin.customers.list.empty_filtered_desc')",
		SQL: mustSQL("431_i18n_customers_status_help.sql"),
	})
}

func init() { registerCustomersStatusHelpI18n() }
