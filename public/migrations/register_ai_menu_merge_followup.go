package migrations

import "sync"

func registerAIMenuMergeFollowup() {
	registerAIMenuMergeFollowupOnce.Do(registerAIMenuMergeFollowupSeed)
}

var registerAIMenuMergeFollowupOnce sync.Once

func registerAIMenuMergeFollowupSeed() {
	registerSeed(Seed{
		Version:   "534-ai-menu-merge-followup",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_menus WHERE path = '/admin/ai/providers' AND deleted_at IS NULL) = 0 " +
			"AND (SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'admin.ai.lead' " +
			"AND lang = 'zh-CN' AND item_value LIKE '两个标签：%') = 1 " +
			"AND (SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'admin.ai.lead' " +
			"AND lang = 'en-US' AND item_value LIKE 'Two tabs:%') = 1 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("534_ai_menu_merge_followup.sql"),
	})
}
