package migrations

import "sync"

// register_dashboard_overview_i18n.go — 概览页词条（540）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：挑两个**代表 key**（各自的中英两条都必须在），
// 本批共 19 个新 key × 2 语言 = 38 条元组，用「代表 key 的语言齐全」当门槛 ——
// 不枚举全部 26 条，也不用全库计数 / LIKE 前缀（存量库永远满足 → 补词条的那条迁移
// 永远不会执行，迁移 494 记过这个坑）。
//
// 偏差方向刻意选「宁可重跑，不可静默跳过」：代表 key 齐全即视为整批已完成；
// INSERT 侧 ON CONFLICT DO NOTHING、UPDATE 侧带「值不同」谓词，重跑幂等。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerDashboardOverviewI18n() {
	registerDashboardOverviewI18nOnce.Do(registerDashboardOverviewI18nSeed)
}

// registerDashboardOverviewI18nOnce 让重复调用成为空操作。
var registerDashboardOverviewI18nOnce sync.Once

// registerDashboardOverviewI18nSeed 注册 540（真正干活的那一半）。
func registerDashboardOverviewI18nSeed() {
	registerSeed(Seed{
		Version:   "540-dashboard-overview-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.kpi.salesToday', 'admin.dashboard.trend.title') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("540_i18n_seed_dashboard_overview.sql"),
	})
}
