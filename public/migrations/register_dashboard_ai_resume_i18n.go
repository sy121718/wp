package migrations

import "sync"

func registerDashboardAIResumeI18n() {
	registerDashboardAIResumeI18nOnce.Do(registerDashboardAIResumeI18nSeed)
}

var registerDashboardAIResumeI18nOnce sync.Once

func registerDashboardAIResumeI18nSeed() {
	registerSeed(Seed{
		Version:   "576-dashboard-ai-resume-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("576_dashboard_ai_resume_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.ai.resume') AND lang IN ('zh-CN', 'en-US')",
	})
}
