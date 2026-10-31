package migrations

import "sync"

func registerSystemSettingsMenu() {
	registerSystemSettingsMenuOnce.Do(registerSystemSettingsMenuSeed)
}

var registerSystemSettingsMenuOnce sync.Once

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
