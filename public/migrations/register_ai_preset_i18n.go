package migrations

import "sync"

func registerAIPresetI18n() {
	registerAIPresetI18nOnce.Do(registerAIPresetI18nSeed)
}

var registerAIPresetI18nOnce sync.Once

func registerAIPresetI18nSeed() {
	registerSeed(Seed{
		Version:   "517-ai-preset-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.tab.preset', 'ai.msg.modelsAppended') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("517_ai_preset_i18n.sql"),
	})
}
