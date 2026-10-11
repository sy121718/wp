package migrations

// 594 · 区块批量删除的独立权限点（block:bulk_delete）—— 长期定义。
//
// 为什么这条 seed 必须存在（而不是只靠启动期 SyncToDB 自动 upsert 权限点）：
// 权限点行本身确实由 SyncToDB 补，但超管策略的**同批写入**与「权限点已存在则一个字段都不动」
// 这两条语义要在一个可审计的地方说清楚；更重要的是 594 与 596 的分工要在注册表里可见
// （596 才是一次性升级复制，见 register_block_bulk_delete_grant_copy.go）。
//
// 判定条件与 SQL 逐字对齐（本文件最容易出的错就是两处判据不一致）：
//
//	SQL 只保证两件事 —— ① 权限点行**存在**（NOT EXISTS permission_code）；② **权限点启用期间**
//	启用超管不缺这条策略（取权限点当前的 api_path/api_method，与 SyncToDB 的补插口径一致）。
//	因此 ConditionSQL 也只判这两件：
//	  · 不判 status / api_path / api_method：api_path 是派生列（由 SyncToDB 对齐），status 是
//	    人工列（运维停用后不该被迁移改回来）。旧写法要求「status=1 且路径正确」，于是行存在
//	    但被停用/路径被改时条件永远为 0 —— 这段 SQL 每次启动都重跑一遍（永久重放）。
//	  · 权限点被停用时，超管那半也判为「无需再补」→ 条件成立 → 整条 seed 跳过。
//
// 一次性历史复制**不在这里**（曾在第一版里，缺陷是 RunSeeds 每次启动重放 → 撤权被授回、
// 新授单删被自动扩权）。SQL 里的 NOT EXISTS 只比对 ptype/v0/v1/v2/v3：v4/v5 在历史行里是
// NULL，唯一索引对 NULL 不判等，带上 v4/v5 的判据会重复插入（见 SQL 文件头的说明）。
//
// ConditionSQL 由 db.Raw 直接执行、没有参数替换，所以整段写成 SQL 字面量。
//
// 本函数由 register.go 的 init() 末尾显式调用（与 593 / 596 同款）：注册点集中在一处，
// 新增迁移要改 register.go，而不是靠文件名顺序隐式生效。
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
