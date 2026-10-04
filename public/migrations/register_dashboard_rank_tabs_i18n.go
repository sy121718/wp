package migrations

import "sync"

// register_dashboard_rank_tabs_i18n.go — 概览页排行榜双 Tab 的词条（553）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：挑两个代表 key，各自的中英两条都必须在
// （13 键 × 2 语言 = 26 条元组，代表 key 语言齐全即认完成）。
//
// pageKind.* 那六条在模板里是**动态 key**（`.t(it.KindKey, …)`），静态扫描看不到，
// 但它们是本批新增的真实词条，一起计入判据才不会被漏掉。
func registerDashboardRankTabsI18n() {
	registerDashboardRankTabsI18nOnce.Do(registerDashboardRankTabsI18nSeed)
}

// registerDashboardRankTabsI18nOnce 让重复调用成为空操作。
var registerDashboardRankTabsI18nOnce sync.Once

// registerDashboardRankTabsI18nSeed 注册 553（真正干活的那一半）。
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
