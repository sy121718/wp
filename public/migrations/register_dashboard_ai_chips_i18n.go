package migrations

import "sync"

// register_dashboard_ai_chips_i18n.go — 概览页 AI 区的说明与快捷提问（575）。
//
// 五条快捷提问是这页能力的说明书：空输入框不会告诉用户「你能问什么」，
// 而四条到五条真实示例可以。缺了它们页面仍能用，只是这一页的能力没人发现。
func registerDashboardAIChipsI18n() {
	registerDashboardAIChipsI18nOnce.Do(registerDashboardAIChipsI18nSeed)
}

var registerDashboardAIChipsI18nOnce sync.Once

func registerDashboardAIChipsI18nSeed() {
	registerSeed(Seed{
		Version:   "575-dashboard-ai-chips-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("575_dashboard_ai_chips_i18n.sql"),
		// 判据取本批自己的六条 key（hint + chip1..5）× 两种语言 = 12 行（用 >= 的理由同 573）。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.ai.hint', 'admin.dashboard.ai.chip1', " +
			"'admin.dashboard.ai.chip2', 'admin.dashboard.ai.chip3', " +
			"'admin.dashboard.ai.chip4', 'admin.dashboard.ai.chip5') " +
			"AND lang IN ('zh-CN', 'en-US')",
	})
}
