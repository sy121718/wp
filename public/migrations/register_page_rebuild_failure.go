package migrations

func init() {
	register(Migration{
		Version:   "474-page-rebuild-failure",
		TableName: "pages",

		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_name = 'pages' AND column_name IN ('rebuild_failed_at', 'rebuild_failed_stage')",
		SQL: mustSQL("474_page_rebuild_failure.sql"),
	})
}
