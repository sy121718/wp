package migrations

import "sync"

func registerDashboardRankTabsI18n() {
	registerDashboardRankTabsI18nOnce.Do(registerDashboardRankTabsI18nSeed)
}

var registerDashboardRankTabsI18nOnce sync.Once

func registerDashboardRankTabsI18nSeed() {
	registerSeed(Seed{
		Version:   "553-dashboard-rank-tabs-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.rank.tabPages', 'admin.dashboard.pageKind.article') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("553_dashboard_rank_tabs_i18n.sql"),
	})
}
