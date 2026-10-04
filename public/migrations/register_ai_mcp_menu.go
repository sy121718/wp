package migrations

import "sync"

// register_ai_mcp_menu.go — 「MCP 与外部访问」菜单项（544）。
//
// 判据只枚举本批自己的对象（上界封闭）：本批只有这一条菜单行，按 path 判定 ——
// path 是页面身份的唯一键（title 可能重名，且 title 将来要按语言调整）。
func registerAIMcpMenu() {
	registerAIMcpMenuOnce.Do(registerAIMcpMenuSeed)
}

// registerAIMcpMenuOnce 让重复调用成为空操作。
var registerAIMcpMenuOnce sync.Once

// registerAIMcpMenuSeed 注册 544（真正干活的那一半）。
func registerAIMcpMenuSeed() {
	registerSeed(Seed{
		Version:   "544-ai-mcp-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE path = '/admin/ai/mcp' AND type = 2 AND deleted_at IS NULL) >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("544_ai_mcp_menu.sql"),
	})
}
