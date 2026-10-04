package migrations

import "sync"

// register_dashboard_kpi_i18n.go — 概览页新增两格 KPI 的词条（551）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：挑商品总量与页面浏览两个代表 key，
// 各自的中英两条都必须在（4 键 × 2 语言 = 8 条元组，代表 key 语言齐全即认完成）。
func registerDashboardKpiI18n() {
	registerDashboardKpiI18nOnce.Do(registerDashboardKpiI18nSeed)
}

// registerDashboardKpiI18nOnce 让重复调用成为空操作。
var registerDashboardKpiI18nOnce sync.Once

// registerDashboardKpiI18nSeed 注册 551（真正干活的那一半）。
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
