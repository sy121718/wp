package migrations

// register_menu_permission.go — 470：菜单 ↔ 权限点多对多关联表。
//
// 注册方式：本文件自带 init()，不在 register.go 的 init() 里再加一行 ——
// 「谁负责注册」只能有一个真源（与 462 / 460a / 459 的既有写法同形）。
//
// 用默认的「表存在即跳过」检查（Migration 不填 ConditionSQL 时的默认行为）：
// 这条迁移的全部效果都挂在 sys_menu_permission 上，表在即已应用。
// 触发器与索引的重建由 SQL 内部的 IF NOT EXISTS / DROP IF EXISTS 保证幂等 ——
// 默认检查在表已存在时会整条跳过，所以这两件事不能只依赖它。
func init() {
	register(Migration{
		Version:   "470-sys-menu-permission",
		TableName: "sys_menu_permission",
		SQL:       mustSQL("470_sys_menu_permission.sql"),
	})
}
