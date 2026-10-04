package migrations

import "sync"

// register_customer_overview_i18n.go — 客户概览页的词条（556）。
//
// 判据按本批自己的对象枚举（上界封闭）：挑两个代表 key（页头空态 + 一个 KPI 标签），
// 各自的中英两条都必须在（20 键 × 2 语言 = 40 条元组，代表 key 语言齐全即认完成）。
//
// 页头说明段是**四个 key 拼一句**（中间夹 <strong>），单独挑一个 key 看不出这条句子
// 是否完整 —— 所以代表的取法避开它们：拿 `empty` 与 `repurchaseRate` 这两个自成一体的。
func registerCustomerOverviewI18n() {
	registerCustomerOverviewI18nOnce.Do(registerCustomerOverviewI18nSeed)
}

// registerCustomerOverviewI18nOnce 让重复调用成为空操作。
var registerCustomerOverviewI18nOnce sync.Once

// registerCustomerOverviewI18nSeed 注册 556（真正干活的那一半）。
func registerCustomerOverviewI18nSeed() {
	registerSeed(Seed{
		Version:   "556-customer-overview-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.customer.overview.empty', 'admin.customer.overview.repurchaseRate') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("556_customer_overview_i18n.sql"),
	})
}
