package migrations

import "sync"

// register_dashboard_page_kind_notfound_i18n.go — 页面类型标签 notFound（554）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：中英两条必须在。
func registerDashboardPageKindNotFoundI18n() {
	registerDashboardPageKindNotFoundI18nOnce.Do(registerDashboardPageKindNotFoundI18nSeed)
}

// registerDashboardPageKindNotFoundI18nOnce 让重复调用成为空操作。
var registerDashboardPageKindNotFoundI18nOnce sync.Once

// registerDashboardPageKindNotFoundI18nSeed 注册 554（真正干活的那一半）。
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
