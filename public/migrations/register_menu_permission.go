package migrations

func init() {
	register(Migration{
		Version:   "470-sys-menu-permission",
		TableName: "sys_menu_permission",
		SQL:       mustSQL("470_sys_menu_permission.sql"),
	})
}
