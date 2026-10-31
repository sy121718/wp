package migrations

import "sync"

func registerCustomerRfmFilterI18n() {
	registerCustomerRfmFilterI18nOnce.Do(registerCustomerRfmFilterI18nSeed)
}

var registerCustomerRfmFilterI18nOnce sync.Once

func registerCustomerRfmFilterI18nSeed() {
	registerSeed(Seed{
		Version:   "564-customer-rfm-filter-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customers.field.rfm', 'admin.customers.rfm.note') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("564_customer_rfm_filter_i18n.sql"),
	})
}
