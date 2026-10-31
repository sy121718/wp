package migrations

import "sync"

func registerAILeadI18n() {
	registerAILeadI18nOnce.Do(registerAILeadI18nSeed)
}

var registerAILeadI18nOnce sync.Once

func registerAILeadI18nSeed() {
	registerSeed(Seed{
		Version:   "533-ai-lead-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key = 'admin.ai.lead' " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("533_ai_lead_i18n.sql"),
	})
}
