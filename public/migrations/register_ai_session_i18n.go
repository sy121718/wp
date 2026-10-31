package migrations

import "sync"

func registerAISessionI18n() {
	registerAISessionI18nOnce.Do(registerAISessionI18nSeed)
}

var registerAISessionI18nOnce sync.Once

func registerAISessionI18nSeed() {
	registerSeed(Seed{
		Version:   "516-ai-session-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.session.title', 'admin.ai.session.fold.reason.not_positive') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("516_ai_session_i18n.sql"),
	})
}
