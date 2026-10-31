package migrations

import "sync"

func registerAIFabDismissI18n() {
	registerAIFabDismissI18nOnce.Do(registerAIFabDismissI18nSeed)
}

var registerAIFabDismissI18nOnce sync.Once

func registerAIFabDismissI18nSeed() {
	registerSeed(Seed{
		Version:   "573-ai-fab-dismiss-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("573_ai_fab_dismiss_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.dashboard.ai.dismiss', 'admin.ai.fab.backToDashboard') " +
			"AND lang IN ('zh-CN', 'en-US')",
	})
}
