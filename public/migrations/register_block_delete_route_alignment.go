package migrations

// registerBlockDeleteRouteAlignment 修 594/597 历史窗口里确认过的路径漂移。
//
// 条件按「当前是否仍漂移」判定：已对齐时直接跳过，不重复写库。
func init() {
	register(Migration{
		Version:   "744-block-delete-route-alignment",
		TableName: "sys_permission",
		CheckSQL: `SELECT CASE WHEN
    NOT EXISTS (
        SELECT 1 FROM sys_permission
        WHERE permission_code = 'block:delete'
          AND (api_path <> '/api/block/delete' OR api_method <> 'POST'))
    AND NOT EXISTS (
        SELECT 1 FROM sys_permission
        WHERE permission_code = 'block:bulk_delete'
          AND (api_path <> '/api/block/bulk-delete' OR api_method <> 'POST'))
    AND NOT EXISTS (
        SELECT 1 FROM sys_casbin_rule r
        JOIN sys_permission p ON p.permission_code = r.v3
        WHERE r.ptype = 'p'
          AND r.v3 IN ('block:delete', 'block:bulk_delete')
          AND (r.v1 <> p.api_path OR r.v2 <> p.api_method))
THEN 1 ELSE 0 END`,
		SQL: mustSQL("744_block_delete_route_alignment.sql"),
	})
}
