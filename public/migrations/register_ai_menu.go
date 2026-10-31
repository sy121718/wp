package migrations

import "sync"

func registerAIMenu() {
	registerAIMenuOnce.Do(registerAIMenuSeed)
}

var registerAIMenuOnce sync.Once

func registerAIMenuSeed() {
	registerSeed(Seed{
		Version:   "515-ai-menu",
		TableName: "sys_menus",

		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_menus WHERE path IN ('/admin/ai/providers', '/admin/ai/sessions') " +
			"AND deleted_at IS NULL) >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menu_permission mp JOIN sys_menus m ON m.id = mp.menu_id " +
			"WHERE m.path IN ('/admin/ai/providers', '/admin/ai/sessions') AND m.deleted_at IS NULL " +
			"AND mp.permission_code IN ('ai:provider_list', 'ai:session_list')) >= 2 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("515_ai_menu.sql"),
	})
}
