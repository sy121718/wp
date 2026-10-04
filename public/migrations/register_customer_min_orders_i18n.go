package migrations

import "sync"

// register_customer_min_orders_i18n.go — 客户列表「复购次数」筛选的词条（563）。
//
// 判据按本批自己的对象枚举（上界封闭）：挑首尾两个代表 key（第一个标签 + 最后一档），
// 各自的中英两条都必须在（7 键 × 2 语言 = 14 条，代表 key 语言齐全即认完成）。
func registerCustomerMinOrdersI18n() {
	registerCustomerMinOrdersI18nOnce.Do(registerCustomerMinOrdersI18nSeed)
}

// registerCustomerMinOrdersI18nOnce 让重复调用成为空操作。
var registerCustomerMinOrdersI18nOnce sync.Once

// registerCustomerMinOrdersI18nSeed 注册 563（真正干活的那一半）。
func registerCustomerMinOrdersI18nSeed() {
	registerSeed(Seed{
		Version:   "563-customer-min-orders-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customers.field.min_orders', 'admin.customers.minOrders.note') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("563_customer_min_orders_i18n.sql"),
	})
}
