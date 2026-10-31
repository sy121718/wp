package migrations

func init() {
	register(Migration{
		Version:   "700-presentation-publication-receipt",
		TableName: "presentation_publications",
		CheckSQL: `SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns c
			WHERE c.table_schema = current_schema()
			  AND c.table_name = ?
			  AND c.column_name = 'receipt_id'
			  AND c.data_type = 'bigint'
			  AND c.is_nullable = 'YES'
			  AND c.column_default IS NULL`,
		SQL: mustSQL("700_presentation_publication_receipt.sql"),
	})
}
