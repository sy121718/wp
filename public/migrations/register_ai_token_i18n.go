package migrations

import "sync"

func registerAITokenI18n() {
	registerAITokenI18nOnce.Do(registerAITokenI18nSeed)
}

var registerAITokenI18nOnce sync.Once

func registerAITokenI18nSeed() {
	registerSeed(Seed{
		Version:   "543-ai-token-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('ai.err.tokenInvalid', 'ai.err.tokenNameRequired') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("543_ai_token_i18n.sql"),
	})
}
