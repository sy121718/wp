package migrations

import "sync"

// register_order_sales_customer_mix_i18n.go — 销售概览「客户类型拆分」的词条（581）。
//
// 与 579 同样是**新增**（ON CONFLICT DO NOTHING），判据也因此看「本批自己的 key 是否都齐」，
// 而不是看值是不是新的。
//
// 按 AGENTS.md「数据库」节的要求枚举本批自己的对象（上界封闭）：不用 LIKE 前缀、
// 也不用全库总量 —— 前缀下已有别的批次的行会让计数虚高 → 本批被静默跳过（058 的形状）。
func registerOrderSalesCustomerMixI18n() {
	registerOrderSalesCustomerMixI18nOnce.Do(registerOrderSalesCustomerMixI18nSeed)
}

var registerOrderSalesCustomerMixI18nOnce sync.Once

func registerOrderSalesCustomerMixI18nSeed() {
	registerSeed(Seed{
		Version:   "581-order-sales-customer-mix-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("581_order_sales_customer_mix_i18n.sql"),
		// 5 个 key × 2 语言 = 10 行。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.order.sales.mix.new'," +
			"'admin.order.sales.mix.returning'," +
			"'admin.order.sales.mix.guest'," +
			"'admin.order.sales.mix.hint'," +
			"'admin.order.sales.monthly.total')",
	})
}
