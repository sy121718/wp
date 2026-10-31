package migrations

import "sync"

func registerAISessionToolsI18n() {
	registerAISessionToolsI18nOnce.Do(registerAISessionToolsI18nSeed)
}

var registerAISessionToolsI18nOnce sync.Once

func registerAISessionToolsI18nSeed() {
	registerSeed(Seed{
		Version:   "539-ai-session-tools-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('ai.err.sessionToolRoundsExceeded', 'ai.err.toolForbidden') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("539_ai_session_tools_i18n.sql"),
	})
}
