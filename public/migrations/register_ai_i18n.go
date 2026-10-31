package migrations

import "sync"

func registerAII18n() {
	registerAII18nOnce.Do(registerAII18nSeed)
}

var registerAII18nOnce sync.Once

func registerAII18nSeed() {
	registerSeed(Seed{
		Version:   "513-ai-i18n-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.title', 'ai.err.internal', 'admin.customers.bulk.result.disabled') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 6 THEN 1 ELSE 0 END",
		SQL: mustSQL("513_ai_i18n_keys.sql"),
	})
}
