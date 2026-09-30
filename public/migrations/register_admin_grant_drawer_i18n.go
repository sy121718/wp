package migrations

// register_admin_grant_drawer_i18n.go — 472：管理员授权分配两个抽屉的界面词条。
//
// 注册方式：本文件自带 init()，不在 register.go 的 init() 里再加一行 ——
// 「谁负责注册」只能有一个真源（与 471 / 469 / 462a 的既有写法同形）。
//
// 门槛判据枚举本批自己的 15 个 key（×2 语言 = 30 行）：不数全库、不按前缀匹配。
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
