package migrations

import "sync"

// register_order_sales_trend_line_i18n.go — 销售概览改折线图后的词条调整（583）。
//
// 本批是**混合**动作（INSERT 新 key + UPDATE 旧文案 + DELETE 孤儿），所以门槛取本批
// 独有的新 key：`admin.order.sales.monthly.title` 只可能由这一批种下，它存在即本批已跑过。
// 不要用 monthly.empty 计数 —— 那是 582 种的，永远满足，迁移就永远不会执行（迁移 494 的形状）。
func registerOrderSalesTrendLineI18n() {
	registerOrderSalesTrendLineI18nOnce.Do(registerOrderSalesTrendLineI18nSeed)
}

var registerOrderSalesTrendLineI18nOnce sync.Once

func registerOrderSalesTrendLineI18nSeed() {
	registerSeed(Seed{
		Version:   "583-order-sales-trend-line-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("583_order_sales_trend_line_i18n.sql"),
		// 1 个新 key × 2 语言 = 2 行。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.order.sales.monthly.title'",
	})
}
