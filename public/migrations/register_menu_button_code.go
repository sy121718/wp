package migrations

import "sync"

func registerMenuButtonCode() {
	registerMenuButtonCodeOnce.Do(registerMenuButtonCodeSeed)
}

var registerMenuButtonCodeOnce sync.Once

func registerMenuButtonCodeSeed() {
	registerSeed(Seed{
		Version:   "589-menu-button-code",
		TableName: "sys_menus",
		SQL:       mustSQL("589_menu_button_code.sql"),
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_menus " +
			"WHERE deleted_at IS NULL AND type = 3 AND (title_key IS NULL OR title_key = '')) = 0 " +
			"THEN 1 ELSE 0 END",
	})
}
