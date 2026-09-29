package migrations

// register_role_permissions_drawer_i18n.go — 迁移 469 的 seed 注册
// （角色权限分配抽屉的成功回执词条）。
//
// 门槛判据**枚举本批自己的唯一一个 item_key**（上界封闭，1 × 2 语言 = 2 行）：
//
//	· 用 LIKE 前缀（`admin.roles.perm.%`）会让 228 已有的 12 个同前缀行把计数抬高，
//	  本批被静默跳过 —— 058 的真实故障；
//	· 用全库总量会在将来新增同前缀 key 时永远追不平，每次启动都重跑 —— 076 的真实故障。
//
// 注册方式：本文件自带 func init()（与 455 / 462a / 464 / 465 一致），不在 register.go 的
// init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
func init() {
	registerSeed(Seed{
		Version:   "469-role-permissions-drawer-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.roles.perm.saved')",
		SQL: mustSQL("469_role_permissions_drawer_i18n.sql"),
	})
}
