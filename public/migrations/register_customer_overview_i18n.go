package migrations

import "sync"

func registerCustomerOverviewI18n() {
	registerCustomerOverviewI18nOnce.Do(registerCustomerOverviewI18nSeed)
}

var registerCustomerOverviewI18nOnce sync.Once

func registerCustomerOverviewI18nSeed() {
	registerSeed(Seed{
		Version:   "556-customer-overview-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customer.overview.empty', 'admin.customer.overview.repurchaseRate') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("556_customer_overview_i18n.sql"),
	})
}
