package migrations

import "sync"

// register_dashboard_trend_granularity_i18n.go — 趋势图粒度切换的词条（578）。
//
// 与 577 的区别：那一条是**改值**（ON CONFLICT DO UPDATE），这一条是**新增**
// （ON CONFLICT DO NOTHING）。判据也因此不同 —— 新增看「本批自己的 key 是否都齐」，
// 改值要看「值是不是新的」。
//
// 判据按 AGENTS.md「数据库」节的要求枚举本批自己的对象（上界封闭），
// 不用 LIKE 前缀、也不用全库总量：前缀下已有别的批次的行会让计数虚高 → 本批被静默跳过。
func registerDashboardTrendGranularityI18n() {
	registerDashboardTrendGranularityI18nOnce.Do(registerDashboardTrendGranularityI18nSeed)
}

var registerDashboardTrendGranularityI18nOnce sync.Once

func registerDashboardTrendGranularityI18nSeed() {
	registerSeed(Seed{
		Version:   "578-dashboard-trend-granularity-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("578_dashboard_trend_granularity_i18n.sql"),
		// 9 个 key × 2 语言 = 18 行；判据取「齐不齐」而不是「够不够多」。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 18 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.dashboard.trend.granularity'," +
			"'admin.dashboard.trend.granularity.hour'," +
			"'admin.dashboard.trend.granularity.day'," +
			"'admin.dashboard.trend.granularity.week'," +
			"'admin.dashboard.trend.granularity.month'," +
			"'admin.dashboard.trend.title.hour'," +
			"'admin.dashboard.trend.title.day'," +
			"'admin.dashboard.trend.title.week'," +
			"'admin.dashboard.trend.title.month')",
	})
}
