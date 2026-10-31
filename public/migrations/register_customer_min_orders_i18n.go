package migrations

import "sync"

func registerCustomerMinOrdersI18n() {
	registerCustomerMinOrdersI18nOnce.Do(registerCustomerMinOrdersI18nSeed)
}

var registerCustomerMinOrdersI18nOnce sync.Once

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
