package migrations

func init() {
	registerSeed(Seed{
		Version:   "469-role-permissions-drawer-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.roles.perm.saved')",
		SQL: mustSQL("469_role_permissions_drawer_i18n.sql"),
	})
}
