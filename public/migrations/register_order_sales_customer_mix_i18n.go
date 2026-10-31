package migrations

import "sync"

func registerOrderSalesCustomerMixI18n() {
	registerOrderSalesCustomerMixI18nOnce.Do(registerOrderSalesCustomerMixI18nSeed)
}

var registerOrderSalesCustomerMixI18nOnce sync.Once

func registerOrderSalesCustomerMixI18nSeed() {
	registerSeed(Seed{
		Version:   "581-order-sales-customer-mix-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("581_order_sales_customer_mix_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.order.sales.mix.new'," +
			"'admin.order.sales.mix.returning'," +
			"'admin.order.sales.mix.guest'," +
			"'admin.order.sales.mix.hint'," +
			"'admin.order.sales.monthly.total')",
	})
}
