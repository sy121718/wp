package migrations

import "sync"

func registerDashboardKpiI18n() {
	registerDashboardKpiI18nOnce.Do(registerDashboardKpiI18nSeed)
}

var registerDashboardKpiI18nOnce sync.Once

func registerDashboardKpiI18nSeed() {
	registerSeed(Seed{
		Version:   "551-dashboard-kpi-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.kpi.items', 'admin.dashboard.kpi.pageViews') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("551_dashboard_kpi_i18n.sql"),
	})
}
