package migrations

import "sync"

// register_customer_rfm_filter_i18n.go — 客户列表「RFM 分段」筛选的词条（564）。
//
// 判据按本批自己的对象枚举（上界封闭）：挑标签与说明两个代表 key，
// 各自的中英两条都必须在（6 键 × 2 语言 = 12 条，代表 key 语言齐全即认完成）。
func registerCustomerRfmFilterI18n() {
	registerCustomerRfmFilterI18nOnce.Do(registerCustomerRfmFilterI18nSeed)
}

// registerCustomerRfmFilterI18nOnce 让重复调用成为空操作。
var registerCustomerRfmFilterI18nOnce sync.Once

// registerCustomerRfmFilterI18nSeed 注册 564（真正干活的那一半）。
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
