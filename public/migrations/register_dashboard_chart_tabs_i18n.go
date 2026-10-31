package migrations

import "sync"

func registerDashboardChartTabsI18n() {
	registerDashboardChartTabsI18nOnce.Do(registerDashboardChartTabsI18nSeed)
}

var registerDashboardChartTabsI18nOnce sync.Once

func registerDashboardChartTabsI18nSeed() {
	registerSeed(Seed{
		Version:   "552-dashboard-chart-tabs-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.chart.tabViews', 'admin.dashboard.chart.viewsAria') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("552_dashboard_chart_tabs_i18n.sql"),
	})
}
