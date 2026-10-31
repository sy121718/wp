package migrations

import "sync"

func registerAIFabI18n() {
	registerAIFabI18nOnce.Do(registerAIFabI18nSeed)
}

var registerAIFabI18nOnce sync.Once

func registerAIFabI18nSeed() {
	registerSeed(Seed{
		Version:   "568-ai-fab-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.fab.title', 'admin.ai.fab.toggle') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("568_ai_fab_i18n.sql"),
	})
}
