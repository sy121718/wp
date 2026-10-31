package migrations

import "sync"

func registerAIUserRequiredI18n() {
	registerAIUserRequiredI18nOnce.Do(registerAIUserRequiredI18nSeed)
}

var registerAIUserRequiredI18nOnce sync.Once

func registerAIUserRequiredI18nSeed() {
	registerSeed(Seed{
		Version:   "536-ai-user-required-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key = 'ai.err.userRequired' " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("536_ai_user_required_i18n.sql"),
	})
}
