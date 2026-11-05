package migrations

func init() {
	registerSeed(Seed{
		Version:   "742-navigation-split-permissions",
		TableName: "sys_permission",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_permission
WHERE permission_code IN ('navigation:move', 'navigation:translate', 'navigation:sourceAdd', 'navigation:deleteBulk')`,
		SQL: mustSQL("742_navigation_split_permissions.sql"),
	})
}
