package migrations

// register_page_rebuild_failure_i18n.go — 475：「最近一次自动重建失败」的界面文案。
//
// 注册方式：本文件自带 init()（与 473/472/471 同形）。
// 门槛判据枚举本批自己的 3 个 key（×2 语言 = 6 行）：不数全库、不按前缀匹配。
func init() {
	registerSeed(Seed{
		Version:   "475-page-rebuild-failure-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.pages.impact.rebuild_failed', 'admin.pages.impact.stage_plan', " +
			"'admin.pages.impact.stage_build')",
		SQL: mustSQL("475_page_rebuild_failure_i18n.sql"),
	})
}
