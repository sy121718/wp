package migrations

func init() {
	registerSeed(Seed{
		Version:   "472-admin-grant-drawer-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 30 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.admins.role.action', 'admin.admins.role.title', " +
			"'admin.admins.role.intro', 'admin.admins.role.empty', 'admin.admins.role.empty.action', " +
			"'admin.admins.role.missing', 'admin.admins.role.save', 'admin.admins.role.saved', " +
			"'admin.admins.menu.action', 'admin.admins.menu.title', 'admin.admins.menu.intro', " +
			"'admin.admins.menu.hint', 'admin.admins.menu.inherited', 'admin.admins.menu.save', " +
			"'admin.admins.menu.saved')",
		SQL: mustSQL("472_admin_grant_drawer_i18n.sql"),
	})
}
