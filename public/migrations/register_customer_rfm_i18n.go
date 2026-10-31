package migrations

import "sync"

func registerCustomerRfmI18n() {
	registerCustomerRfmI18nOnce.Do(registerCustomerRfmI18nSeed)
}

var registerCustomerRfmI18nOnce sync.Once

func registerCustomerRfmI18nSeed() {
	registerSeed(Seed{
		Version:   "560-customer-rfm-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customer.rfm.col.segment', 'admin.customer.rfm.segment.vip') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("560_customer_rfm_i18n.sql"),
	})
}
