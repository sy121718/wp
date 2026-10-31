package migrations

func registerBlockBulkDeletePermission() {
	registerSeed(Seed{
		Version:   "594-block-bulk-delete-permission",
		TableName: "sys_permission",
		ConditionSQL: `SELECT CASE WHEN
    EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = 'block:bulk_delete')
    AND NOT EXISTS (
        SELECT 1 FROM sys_admin a
        JOIN sys_permission p
          ON p.permission_code = 'block:bulk_delete' AND p.status = 1
        WHERE a.is_admin = 1 AND a.status = 1
          AND NOT EXISTS (
              SELECT 1 FROM sys_casbin_rule r
              WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code))
THEN 1 ELSE 0 END`,
		SQL: mustSQL("594_block_bulk_delete_permission.sql"),
	})
}
