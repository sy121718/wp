package migrations

import "sync"

func registerCustomerTierFilterI18n() {
	registerCustomerTierFilterI18nOnce.Do(registerCustomerTierFilterI18nSeed)
}

var registerCustomerTierFilterI18nOnce sync.Once

func registerCustomerTierFilterI18nSeed() {
	registerSeed(Seed{
		Version:   "565-customer-tier-filter-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customers.field.tier', 'admin.customers.tier.note') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("565_customer_tier_filter_i18n.sql"),
	})
}
