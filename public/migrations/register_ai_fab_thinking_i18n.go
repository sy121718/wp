package migrations

import "sync"

// register_ai_fab_thinking_i18n.go — 悬浮球的「思考中」与「思考过程」词条（570）。
func registerAIFabThinkingI18n() {
	registerAIFabThinkingI18nOnce.Do(registerAIFabThinkingI18nSeed)
}

var registerAIFabThinkingI18nOnce sync.Once

func registerAIFabThinkingI18nSeed() {
	registerSeed(Seed{
		Version:   "570-ai-fab-thinking-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("570_ai_fab_thinking_i18n.sql"),
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.fab.thinking', 'ai.fab.thinkLabel') " +
			"AND lang IN ('zh-CN', 'en-US')",
	})
}
