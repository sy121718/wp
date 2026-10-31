package migrations

func init() {
	registerSeed(Seed{
		Version:   "471-menu-permission-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.menus.col.permissions', 'admin.menus.field.permissions', " +
			"'admin.menus.field.permissions_hint', 'admin.menus.field.permissions_search', " +
			"'admin.menus.field.permissions_empty')",
		SQL: mustSQL("471_menu_permission_i18n.sql"),
	})
}
