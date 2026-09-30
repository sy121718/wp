package migrations

// register_menu_permission_i18n.go — 471：菜单绑定权限点（多对多）的界面词条。
//
// 注册方式：本文件自带 init()，不在 register.go 的 init() 里再加一行 ——
// 「谁负责注册」只能有一个真源（与 469 / 462a / 460a 的既有写法同形）。
//
// 门槛判据枚举本批自己的 5 个 key（×2 语言 = 10 行）：不数全库、不按前缀匹配。
// 偏差方向刻意选「宁可重跑」——词条少一条只是那一句回退成模板里的中文兜底，
// 而漏 seed 会让中英成对判据（admin_group_f_i18n_test.go）直接变红。
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
