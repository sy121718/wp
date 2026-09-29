package migrations

// register_comment_menu.go — 评论审核的后台菜单（迁移 467，BIZ-5）。
//
// 与 463（会员菜单）同构：sys_menus 的幂等 seed，挂「内容」目录下。
// 用 registerSeed 而不是 register —— 菜单是「可重复写入的默认值」，与权限点 seed 同类。
//
// 门槛判据**逐条枚举本批自己的那个菜单项**（上界封闭）：按 path 判定而不是 title
// （标题可能重名，path 才是这条菜单的实际落点）。既不用 LIKE 前缀也不用全库总量：
//   - 前缀（path LIKE '/admin/comment%'）会在将来新增评论子菜单时计数虚高 → 本批被静默跳过；
//   - 全库总量（sys_menus 总行数）会被任何一次菜单增删打破 → 每次启动重跑。
//
// 注册方式：本文件自带 init()，不在 register.go 的 init() 里再加一行。
func init() {
	registerSeed(Seed{
		Version:   "467-comment-menu",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus " +
			"WHERE type = 2 AND deleted_at IS NULL AND path IN ('/admin/comments')",
		SQL: mustSQL("467_comment_menu.sql"),
	})
}
