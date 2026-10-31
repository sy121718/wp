package migrations

import "sync"

func registerDashboardAIChipsI18n() {
	registerDashboardAIChipsI18nOnce.Do(registerDashboardAIChipsI18nSeed)
}

var registerDashboardAIChipsI18nOnce sync.Once

func registerDashboardAIChipsI18nSeed() {
	registerSeed(Seed{
		Version:   "575-dashboard-ai-chips-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("575_dashboard_ai_chips_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.ai.hint', 'admin.dashboard.ai.chip1', " +
			"'admin.dashboard.ai.chip2', 'admin.dashboard.ai.chip3', " +
			"'admin.dashboard.ai.chip4', 'admin.dashboard.ai.chip5') " +
			"AND lang IN ('zh-CN', 'en-US')",
	})
}
