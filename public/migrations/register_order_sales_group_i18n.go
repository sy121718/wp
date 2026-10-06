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
		// 1 个 key × 2 语言 = 2 行。
		//
		// **门槛必须与 SQL 里的 INSERT 同一批收窄**：582 原本还种了「销售 / 客户」两个
		// 分组标题 key，583 把它们删了（卡片合成一排四张后没有分组标题）。若这里仍按
		// 3 个 key 计数，门槛永远不满足 —— 每次启动都会重跑 582，把删掉的两条插回来。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.order.sales.monthly.empty'",
	})
}
