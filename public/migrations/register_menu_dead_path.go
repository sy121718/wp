package migrations

import "sync"

func registerMenuDeadPath() {
	registerMenuDeadPathOnce.Do(registerMenuDeadPathSeed)
}

var registerMenuDeadPathOnce sync.Once

func registerMenuDeadPathSeed() {
	registerSeed(Seed{
		Version:   "588-menu-dead-path-clear",
		TableName: "sys_menus",
		SQL:       mustSQL("588_menu_dead_path_clear.sql"),
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE deleted_at IS NULL AND is_hidden = 1 AND type = 2 " +
			"AND path IN ('/project', '/artifact', '/publication')) = 0 THEN 1 ELSE 0 END",
	})
}
