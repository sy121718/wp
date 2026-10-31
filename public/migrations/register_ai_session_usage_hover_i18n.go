package migrations

import "sync"

func registerAISessionUsageHoverI18n() {
	registerAISessionUsageHoverI18nOnce.Do(registerAISessionUsageHoverI18nSeed)
}

var registerAISessionUsageHoverI18nOnce sync.Once

func registerAISessionUsageHoverI18nSeed() {
	registerSeed(Seed{
		Version:   "530-ai-session-usage-hover-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.session.usage.breakdown', 'admin.ai.session.usage.unrecorded') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("530_ai_session_usage_hover_i18n.sql"),
	})
}
