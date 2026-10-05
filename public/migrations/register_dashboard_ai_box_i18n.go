package migrations

import "sync"

// register_dashboard_ai_box_i18n.go — 概览页的 AI 提问区与「销售数据」卡词条（571）。
func registerDashboardAIBoxI18n() {
	registerDashboardAIBoxI18nOnce.Do(registerDashboardAIBoxI18nSeed)
}

var registerDashboardAIBoxI18nOnce sync.Once

func registerDashboardAIBoxI18nSeed() {
	registerSeed(Seed{
		Version:   "571-dashboard-ai-box-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("571_dashboard_ai_box_i18n.sql"),
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.kpi.salesData', " +
			"'admin.dashboard.ai.title', 'admin.dashboard.ai.placeholder') " +
			"AND lang IN ('zh-CN', 'en-US')",
	})
}
