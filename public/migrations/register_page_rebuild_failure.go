package migrations

// register_page_rebuild_failure.go — 474：页面记住最近一次自动重建失败的阶段与时刻。
//
// 注册方式：本文件自带 init()，不在 register.go 的 init() 里再加一行 ——
// 「谁负责注册」只能有一个真源（与 470 / 462 / 460a 的既有写法同形）。
//
// 用「列存在即跳过」的默认检查（Migration 不填 ConditionSQL 的默认行为）。
func init() {
	register(Migration{
		Version:   "474-page-rebuild-failure",
		TableName: "pages",
		// 默认检查只判「表在不在」，而 pages 早就存在 —— 那会让这条迁移永远走
		// 「已存在、跳过」，新列永远建不出来。显式给列级检查（不带 ? 占位符：
		// 带占位符时 migrator 会传入表名，而这里是对固定表的两次列名判定）。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_name = 'pages' AND column_name IN ('rebuild_failed_at', 'rebuild_failed_stage')",
		SQL: mustSQL("474_page_rebuild_failure.sql"),
	})
}
