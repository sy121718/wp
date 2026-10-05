package migrations

import "sync"

// register_ai_fab_image_i18n.go — 悬浮球的图片附件词条（572）。
func registerAIFabImageI18n() {
	registerAIFabImageI18nOnce.Do(registerAIFabImageI18nSeed)
}

var registerAIFabImageI18nOnce sync.Once

func registerAIFabImageI18nSeed() {
	registerSeed(Seed{
		Version:   "572-ai-fab-image-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("572_ai_fab_image_i18n.sql"),
		// 判据覆盖本批**自己的**五个 key（模板侧两个 + handler 侧三个）。
		// 用 >= 而不是 = ：将来若有人往同一条迁移里补词条，等号会让判据
		// 从「已执行」变成「未执行」而整条重跑（虽然 ON CONFLICT 兜住了，
		// 但那等于每次启动都白跑一遍）。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 10 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.fab.pickImage', 'admin.ai.fab.imageRemove', " +
			"'ai.fab.imageBadFormat', 'ai.fab.imageTooMany', 'ai.fab.imageTooLarge') " +
			"AND lang IN ('zh-CN', 'en-US')",
	})
}
