package migrations

import "sync"

func registerAIFabImageI18n() {
	registerAIFabImageI18nOnce.Do(registerAIFabImageI18nSeed)
}

var registerAIFabImageI18nOnce sync.Once

func registerAIFabImageI18nSeed() {
	registerSeed(Seed{
		Version:   "572-ai-fab-image-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("572_ai_fab_image_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.fab.pickImage', 'admin.ai.fab.imageRemove', " +
			"'ai.fab.imageBadFormat', 'ai.fab.imageTooMany', 'ai.fab.imageTooLarge') " +
			"AND lang IN ('zh-CN', 'en-US')",
	})
}
