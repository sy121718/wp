package migrations

import "sync"

// register_dashboard_kpi_wording_i18n.go — 概览页 KPI 的措辞（577）。
//
// 与 570/571/575 一批 seed 的区别：那几条是**新增**词条（ON CONFLICT DO NOTHING），
// 这条是**改值**（ON CONFLICT DO UPDATE）—— 「净销售额」在 570 里已经写进去过，
// 只 INSERT 不动存量，老库上永远改不过来。
func registerDashboardKPIWordingI18n() {
	registerDashboardKPIWordingI18nOnce.Do(registerDashboardKPIWordingI18nSeed)
}

var registerDashboardKPIWordingI18nOnce sync.Once

func registerDashboardKPIWordingI18nSeed() {
	registerSeed(Seed{
		Version:   "577-dashboard-kpi-wording-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("577_dashboard_kpi_wording_i18n.sql"),
		// 判据必须看**值**而不是行数：行数一直都在（570 就插过了），
		// 只有值是新的才说明这条改值迁移跑过。这正是「跳过」与「已生效」的区别。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key = 'admin.dashboard.kpi.sales' AND item_value IN ('销售额', 'Sales'))",
	})
}
