package migrations

import "sync"

func registerAISessionUsageI18n() {
	registerAISessionUsageI18nOnce.Do(registerAISessionUsageI18nSeed)
}

var registerAISessionUsageI18nOnce sync.Once

func registerAISessionUsageI18nSeed() {
	registerSeed(Seed{
		Version:   "527-ai-session-usage-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.tab.sessions', 'admin.ai.session.trend.peak') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("527_ai_session_usage_i18n.sql"),
	})
}
