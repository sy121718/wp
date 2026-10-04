package migrations

import "sync"

// register_dashboard_new_customers_kpi_i18n.go — 概览页「新客户」KPI 的词条（557）。
//
// 判据：两个 key 的中英四条必须都在（2 键 × 2 语言 = 4 条元组）。
func registerDashboardNewCustomersKPI18n() {
	registerDashboardNewCustomersKPI18nOnce.Do(registerDashboardNewCustomersKPI18nSeed)
}

// registerDashboardNewCustomersKPI18nOnce 让重复调用成为空操作。
var registerDashboardNewCustomersKPI18nOnce sync.Once

// registerDashboardNewCustomersKPI18nSeed 注册 557（真正干活的那一半）。
func registerDashboardNewCustomersKPI18nSeed() {
	registerSeed(Seed{
		Version:   "557-dashboard-new-customers-kpi-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.kpi.newCustomers') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("557_dashboard_new_customers_kpi_i18n.sql"),
	})
}
