package migrations

import "sync"

// register_dashboard_range_i18n.go — 概览页时间筛选条与区间口径的词条（550）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：挑筛选条的自定义键与趋势的按周标题
// 两个代表 key，各自的中英两条都必须在（15 键 × 2 语言 = 30 条元组，代表 key
// 语言齐全即认完成）。不用全库计数 / LIKE 前缀 —— 存量库永远满足，补词条的那条
// 迁移就永远不会执行（迁移 494 记过这个坑）。
//
// 注意 ConditionSQL 里的**代表 key 必须选本批新增的**：550 同时 UPDATE 了两个既有
// key（trend.title / top.title），拿它们当判据等于「改动前就满足」——判定恒真。
func registerDashboardRangeI18n() {
	registerDashboardRangeI18nOnce.Do(registerDashboardRangeI18nSeed)
}

// registerDashboardRangeI18nOnce 让重复调用成为空操作。
var registerDashboardRangeI18nOnce sync.Once

// registerDashboardRangeI18nSeed 注册 550（真正干活的那一半）。
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
