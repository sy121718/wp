package migrations

import "sync"

func registerDashboardOverviewI18n() {
	registerDashboardOverviewI18nOnce.Do(registerDashboardOverviewI18nSeed)
}

var registerDashboardOverviewI18nOnce sync.Once

func registerDashboardOverviewI18nSeed() {
	registerSeed(Seed{
		Version:   "540-dashboard-overview-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.kpi.salesToday', 'admin.dashboard.trend.title') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("540_i18n_seed_dashboard_overview.sql"),
	})
}
