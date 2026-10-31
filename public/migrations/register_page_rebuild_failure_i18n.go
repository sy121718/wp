package migrations

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
