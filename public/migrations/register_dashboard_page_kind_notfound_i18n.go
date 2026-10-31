package migrations

import "sync"

func registerDashboardPageKindNotFoundI18n() {
	registerDashboardPageKindNotFoundI18nOnce.Do(registerDashboardPageKindNotFoundI18nSeed)
}

var registerDashboardPageKindNotFoundI18nOnce sync.Once

func registerDashboardPageKindNotFoundI18nSeed() {
	registerSeed(Seed{
		Version:   "554-dashboard-page-kind-notfound-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key = 'admin.dashboard.pageKind.notFound' " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("554_dashboard_page_kind_notfound_i18n.sql"),
	})
}
