package migrations

import "sync"

func registerAnalyticsViewTabsI18n() {
	registerAnalyticsViewTabsI18nOnce.Do(registerAnalyticsViewTabsI18nSeed)
}

var registerAnalyticsViewTabsI18nOnce sync.Once

func registerAnalyticsViewTabsI18nSeed() {

	registerSeed(Seed{
		Version:   "427-analytics-view-tabs-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN ('admin.analytics.view.label')",
		SQL: mustSQL("427_analytics_view_tabs_i18n.sql"),
	})
}

func init() { registerAnalyticsViewTabsI18n() }
