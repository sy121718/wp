package migrations

func init() {
	registerSeed(Seed{
		Version:   "463-membership-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_menus " +
			"WHERE type = 2 AND deleted_at IS NULL " +
			"AND path IN ('/admin/membership', '/admin/membership/assignments')",
		SQL: mustSQL("463_membership_menu.sql"),
	})
}
