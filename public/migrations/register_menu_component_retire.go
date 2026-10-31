package migrations

import "sync"

func registerMenuComponentRetire() {
	registerMenuComponentRetireOnce.Do(registerMenuComponentRetireSeed)
}

var registerMenuComponentRetireOnce sync.Once

func registerMenuComponentRetireSeed() {
	registerSeed(Seed{
		Version:   "587-menu-component-retire",
		TableName: "sys_menus",
		SQL:       mustSQL("587_menu_component_retire.sql"),
		ConditionSQL: "SELECT CASE " +
			"WHEN NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'sys_menus'::regclass " +
			"AND attname = 'component' AND NOT attisdropped) THEN 1 " +
			"WHEN col_description('sys_menus'::regclass, (SELECT attnum FROM pg_attribute " +
			"WHERE attrelid = 'sys_menus'::regclass AND attname = 'component')) IS NOT NULL THEN 1 " +
			"WHEN NOT EXISTS (SELECT 1 FROM sys_i18n WHERE item_key IN " +
			"('ErrComponentRequired', 'ErrComponentNotAllowed', 'ErrComponentInvalid')) THEN 1 " +
			"ELSE 0 END",
	})
}
