package migrations

import "sync"

func registerAISessionDetailI18n() {
	registerAISessionDetailI18nOnce.Do(registerAISessionDetailI18nSeed)
}

var registerAISessionDetailI18nOnce sync.Once

func registerAISessionDetailI18nSeed() {
	registerSeed(Seed{
		Version:   "547-ai-session-detail-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.detail.title', 'admin.ai.detail.noEvents') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("547_ai_session_detail_i18n.sql"),
	})
}
