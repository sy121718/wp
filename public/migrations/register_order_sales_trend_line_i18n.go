package migrations

import "sync"

func registerOrderSalesTrendLineI18n() {
	registerOrderSalesTrendLineI18nOnce.Do(registerOrderSalesTrendLineI18nSeed)
}

var registerOrderSalesTrendLineI18nOnce sync.Once

func registerOrderSalesTrendLineI18nSeed() {
	registerSeed(Seed{
		Version:   "583-order-sales-trend-line-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("583_order_sales_trend_line_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.order.sales.monthly.title'",
	})
}
