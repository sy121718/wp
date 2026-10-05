package migrations

import "sync"

// register_customer_tier_filter_i18n.go — 客户列表「会员等级」筛选的词条（565）。
//
// 判据按本批自己的对象枚举（上界封闭）：标签与说明两个代表 key，
// 各自的中英两条都必须在（3 键 × 2 语言 = 6 条，代表 key 语言齐全即认完成）。
func registerCustomerTierFilterI18n() {
	registerCustomerTierFilterI18nOnce.Do(registerCustomerTierFilterI18nSeed)
}

// registerCustomerTierFilterI18nOnce 让重复调用成为空操作。
var registerCustomerTierFilterI18nOnce sync.Once

// registerCustomerTierFilterI18nSeed 注册 565（真正干活的那一半）。
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
