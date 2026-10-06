package migrations

import "sync"

// register_order_sales_group_i18n.go — 销售概览页的分组标题与空态词条（582）。
//
// 与 579 / 581 同样是新增（ON CONFLICT DO NOTHING），判据看「本批自己的 key 是否都齐」。
// 按 AGENTS.md「数据库」节枚举本批对象（上界封闭）：不用 LIKE 前缀、不用全库总量 ——
// 前缀下已有别的批次的行会让计数虚高 → 本批被静默跳过（058 的形状）。
func registerOrderSalesGroupI18n() {
	registerOrderSalesGroupI18nOnce.Do(registerOrderSalesGroupI18nSeed)
}

var registerOrderSalesGroupI18nOnce sync.Once

func registerOrderSalesGroupI18nSeed() {
	registerSeed(Seed{
		Version:   "582-order-sales-group-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("582_order_sales_group_i18n.sql"),
		// 3 个 key × 2 语言 = 6 行。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.order.sales.group.sales'," +
			"'admin.order.sales.group.customers'," +
			"'admin.order.sales.monthly.empty')",
	})
}
