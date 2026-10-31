package migrations

func init() {
	register(Migration{
		Version:   "699-navigation-input-version",
		TableName: "pages",

		CheckSQL: `SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM (
			SELECT 1 FROM information_schema.columns c
			 WHERE c.table_schema = current_schema() AND c.table_name = ?
			   AND c.column_name = 'input_version' AND c.data_type = 'bigint'
			UNION ALL
			SELECT 1 FROM information_schema.columns c
			 WHERE c.table_schema = current_schema() AND c.table_name = 'presentation_instances'
			   AND c.column_name = 'input_version' AND c.data_type = 'bigint'
			UNION ALL
			SELECT 1 FROM pg_constraint k
			 WHERE k.conname = 'publication_receipts_receipt_state_check'
			   AND pg_get_constraintdef(k.oid) LIKE '%superseded%'
		) present`,
		SQL: mustSQL("699_navigation_input_version.sql"),
	})
}
