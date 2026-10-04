package migrations

import "sync"

// register_dashboard_chart_tabs_i18n.go — 概览页图表双 Tab 的词条（552）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：挑两个代表 key，各自的中英两条都必须在
// （5 键 × 2 语言 = 10 条元组，代表 key 语言齐全即认完成）。
//
// 代表 key 必须是**本批新增的**：552 同时 UPDATE 了三个既有 key（标题与空态），
// 拿它们当判据等于「改动前就满足」—— 判定恒真，这条迁移永远不会跑。
func registerDashboardChartTabsI18n() {
	registerDashboardChartTabsI18nOnce.Do(registerDashboardChartTabsI18nSeed)
}

// registerDashboardChartTabsI18nOnce 让重复调用成为空操作。
var registerDashboardChartTabsI18nOnce sync.Once

// registerDashboardChartTabsI18nSeed 注册 552（真正干活的那一半）。
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
