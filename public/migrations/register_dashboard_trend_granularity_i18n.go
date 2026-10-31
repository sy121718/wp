package migrations

import "sync"

func registerDashboardTrendGranularityI18n() {
	registerDashboardTrendGranularityI18nOnce.Do(registerDashboardTrendGranularityI18nSeed)
}

var registerDashboardTrendGranularityI18nOnce sync.Once

func registerDashboardTrendGranularityI18nSeed() {
	registerSeed(Seed{
		Version:   "578-dashboard-trend-granularity-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("578_dashboard_trend_granularity_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 18 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.dashboard.trend.granularity'," +
			"'admin.dashboard.trend.granularity.hour'," +
			"'admin.dashboard.trend.granularity.day'," +
			"'admin.dashboard.trend.granularity.week'," +
			"'admin.dashboard.trend.granularity.month'," +
			"'admin.dashboard.trend.title.hour'," +
			"'admin.dashboard.trend.title.day'," +
			"'admin.dashboard.trend.title.week'," +
			"'admin.dashboard.trend.title.month')",
	})
}
