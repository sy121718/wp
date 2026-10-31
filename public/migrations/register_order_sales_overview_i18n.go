package migrations

import "sync"

func registerOrderSalesOverviewI18n() {
	registerOrderSalesOverviewI18nOnce.Do(registerOrderSalesOverviewI18nSeed)
}

var registerOrderSalesOverviewI18nOnce sync.Once

func registerOrderSalesOverviewI18nSeed() {
	registerSeed(Seed{
		Version:   "579-order-sales-overview-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("579_order_sales_overview_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 72 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.order.sales.title'," +
			"'admin.order.sales.desc'," +
			"'admin.order.sales.toOrders'," +
			"'admin.order.sales.field.from'," +
			"'admin.order.sales.field.to'," +
			"'admin.order.sales.field.status'," +
			"'admin.order.sales.field.monthly'," +
			"'admin.order.sales.filter.submit'," +
			"'admin.order.sales.status.all'," +
			"'admin.order.sales.loadFailed'," +
			"'admin.order.sales.empty'," +
			"'admin.order.sales.emptyHint'," +
			"'admin.order.sales.label.orders'," +
			"'admin.order.sales.label.sales'," +
			"'admin.order.sales.label.aov'," +
			"'admin.order.sales.label.units'," +
			"'admin.order.sales.label.customers'," +
			"'admin.order.sales.label.new'," +
			"'admin.order.sales.label.returning'," +
			"'admin.order.sales.label.acv'," +
			"'admin.order.sales.note.orders'," +
			"'admin.order.sales.note.sales'," +
			"'admin.order.sales.note.aov'," +
			"'admin.order.sales.note.units'," +
			"'admin.order.sales.note.customers'," +
			"'admin.order.sales.note.new'," +
			"'admin.order.sales.note.returning'," +
			"'admin.order.sales.note.acv'," +
			"'admin.order.sales.compare'," +
			"'admin.order.sales.compareHint'," +
			"'admin.order.sales.compare.metric'," +
			"'admin.order.sales.compare.prev'," +
			"'admin.order.sales.compare.change'," +
			"'admin.order.sales.compare.noBase'," +
			"'admin.order.sales.monthly'," +
			"'admin.order.sales.monthlyHint')",
	})
}
