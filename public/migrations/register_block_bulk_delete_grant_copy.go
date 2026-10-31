package migrations

func registerBlockBulkDeleteGrantCopy() {

	register(Migration{
		Version:   "596-sys-migration-marker",
		TableName: "sys_migration_marker",
		SQL:       mustSQL("596_sys_migration_marker.sql"),
	})

	register(Migration{
		Version:   "597-block-bulk-delete-grant-copy",
		TableName: "sys_migration_marker",
		CheckSQL: `SELECT CASE WHEN EXISTS (
    SELECT 1 FROM sys_migration_marker WHERE version = '597-block-bulk-delete-grant-copy'
) THEN 1 ELSE 0 END`,
		SQL: mustSQL("597_block_bulk_delete_grant_copy.sql"),
	})
}
