package migrations

import "sync"

func registerAIMenuMerge() {
	registerAIMenuMergeOnce.Do(registerAIMenuMergeSeed)
}

var registerAIMenuMergeOnce sync.Once

func registerAIMenuMergeSeed() {
	registerSeed(Seed{
		Version:   "532-ai-menu-merge",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_menus WHERE path = '/admin/ai/sessions' " +
			"AND title = '大模型管理' AND deleted_at IS NULL) >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menus WHERE title = 'AI 会话' AND deleted_at IS NULL) = 0 " +
			"AND (SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ('admin.ai.menu.title', 'admin.ai.noAccess') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("532_ai_menu_merge.sql"),
	})
}
