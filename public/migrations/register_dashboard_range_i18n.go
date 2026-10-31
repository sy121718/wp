package migrations

import "sync"

func registerDashboardRangeI18n() {
	registerDashboardRangeI18nOnce.Do(registerDashboardRangeI18nSeed)
}

var registerDashboardRangeI18nOnce sync.Once

func registerDashboardRangeI18nSeed() {
	registerSeed(Seed{
		Version:   "550-dashboard-range-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.range.custom', 'admin.dashboard.trend.titleWeekly') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("550_dashboard_range_i18n.sql"),
	})
}
