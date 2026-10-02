package migrations

import "sync"

// register_system_settings_menu.go — 系统设置页侧栏入口（498）。
func registerSystemSettingsMenu() {
	registerSystemSettingsMenuOnce.Do(registerSystemSettingsMenuSeed)
}

// registerSystemSettingsMenuOnce 让重复调用成为空操作。
var registerSystemSettingsMenuOnce sync.Once

// registerSystemSettingsMenuSeed 注册 498（真正干活的那一半）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：菜单行在，且权限关联在。
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，值只能写进 SQL 字面量。
func registerSystemSettingsMenuSeed() {
	registerSeed(Seed{
		Version:   "498-system-settings-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus WHERE path = '/admin/system' AND deleted_at IS NULL) >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menu_permission mp JOIN sys_menus m ON m.id = mp.menu_id " +
			"WHERE m.path = '/admin/system' AND mp.permission_code = 'sysconfig:get') >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("498_system_settings_menu.sql"),
	})
}
