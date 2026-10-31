package migrations

import "sync"

func registerAIMcpMenu() {
	registerAIMcpMenuOnce.Do(registerAIMcpMenuSeed)
}

var registerAIMcpMenuOnce sync.Once

func registerAIMcpMenuSeed() {
	registerSeed(Seed{
		Version:   "544-ai-mcp-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE path = '/admin/ai/mcp' AND type = 2 AND deleted_at IS NULL) >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("544_ai_mcp_menu.sql"),
	})
}
