package migrations

import "sync"

func registerOrderSalesGroupI18n() {
	registerOrderSalesGroupI18nOnce.Do(registerOrderSalesGroupI18nSeed)
}

var registerOrderSalesGroupI18nOnce sync.Once

func registerOrderSalesGroupI18nSeed() {
	registerSeed(Seed{
		Version:   "582-order-sales-group-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("582_order_sales_group_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.order.sales.monthly.empty'",
	})
}
