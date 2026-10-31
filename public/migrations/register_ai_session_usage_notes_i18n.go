package migrations

import "sync"

func registerAISessionUsageNotesI18n() {
	registerAISessionUsageNotesI18nOnce.Do(registerAISessionUsageNotesI18nSeed)
}

var registerAISessionUsageNotesI18nOnce sync.Once

func registerAISessionUsageNotesI18nSeed() {
	registerSeed(Seed{
		Version:   "528-ai-session-usage-notes-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.session.usage.total', 'admin.ai.session.usage.compactNote') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("528_ai_session_usage_notes_i18n.sql"),
	})
}
