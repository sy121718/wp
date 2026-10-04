package migrations

import "sync"

// register_ai_menu.go — AI 模块的后台菜单入口（515）。
//
// 判定枚举本批自己的四个对象（上界封闭）：
//
//	· sys_menus 里 path = '/admin/ai/providers' 且有未删行；
//	· sys_menu_permission 里该菜单挂着 ai:provider_list；
//	· sys_menus 里 path = '/admin/ai/sessions' 且有未删行；
//	· sys_menu_permission 里该菜单挂着 ai:session_list。
//
// 四者同时成立才视为整批已完成 —— 与 498（registerSystemSettingsMenu）同形，
// 不用全库计数 / LIKE 前缀（存量库永远满足 → 迁移永远不执行，494 记过这个坑）。
//
// 为什么要四个都判（本轮补会话入口时扩的）：若只判供应商那一组，会话入口是在同一条 SQL
// 里后加的 —— 供应商组已成立时整条会被跳过，后加的会话菜单永远插不进来。
// 判据必须覆盖本批**全部**对象，与 512 两表判据同理（评审 11）。
//
// 本迁移**带 TableName**（sys_menus）：目标库没有 sys_menus 时整条跳过（沿用 498 的口径）。
func registerAIMenu() {
	registerAIMenuOnce.Do(registerAIMenuSeed)
}

// registerAIMenuOnce 让重复调用成为空操作。
var registerAIMenuOnce sync.Once

// registerAIMenuSeed 注册 515（真正干活的那一半）。
func registerAIMenuSeed() {
	registerSeed(Seed{
		Version:   "515-ai-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_menus WHERE path = '/admin/ai/providers' AND deleted_at IS NULL) >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menu_permission mp JOIN sys_menus m ON m.id = mp.menu_id " +
			"WHERE m.path = '/admin/ai/providers' AND mp.permission_code = 'ai:provider_list') >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menus WHERE path = '/admin/ai/sessions' AND deleted_at IS NULL) >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menu_permission mp JOIN sys_menus m ON m.id = mp.menu_id " +
			"WHERE m.path = '/admin/ai/sessions' AND mp.permission_code = 'ai:session_list') >= 1 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("515_ai_menu.sql"),
	})
}
