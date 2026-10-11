package migrations

// 596 / 597 · 一次性数据迁移的「标记表 + 使用者」两条注册项。
//
// 为什么拆成两条（这是审核发现的真实阻断，不是洁癖）：
//
//	PostgreSQL 在**解析/规划**阶段就解析整条 SQL 里的所有 FROM 关系，CASE 的短路只发生在执行期。
//	最初把「建表 + 查标记行」写进同一条迁移的 CheckSQL（`CASE WHEN to_regclass(...) IS NULL
//	THEN 0 WHEN EXISTS (SELECT 1 FROM sys_migration_marker ...) ...`），全新库里表还不存在时，
//	后一个分支的静态子查询在解析期就报 `relation "sys_migration_marker" does not exist` ——
//	to_regclass 那道守卫完全不起作用，迁移直接跑不起来。
//	所以：结构（596，无 CheckSQL → 走默认「表是否已建」检查、建表语句本身又带 IF NOT EXISTS）
//	必须**先于**使用者（597）执行；迁移按 Version 排序执行，596 < 597 保证了这个顺序。
//
// 为什么使用者用 Migration 而不是 Seed：
//
//	ApplyStatements 把「存在性检查 + 全部语句」放在同一个事务里；applySeed 只有「判定 + Exec」。
//	597 由「复制授权」+「写执行标记」两条语句组成，必须同生共死（复制半途失败而标记已落 =
//	永久少一批授权）。用 Seed 就丢掉了这层原子性。
//
// CheckSQL 的语义是「> 0 即跳过」：
//
//	596 用默认检查（表在 → 跳过），597 查标记行（行在 → 跳过）。两者都与「业务数据本身是否还在」
//	无关 —— 运维之后删掉某条策略行，也不会让一次性动作重放（那正是这次要修的缺陷形态）。
func registerBlockBulkDeleteGrantCopy() {
	// 596：标记表结构。CheckSQL 留空 = 使用默认的「主对象是否已建」（ApplyStatements 会把
	// TableName 作为参数传给默认检查），这与建表语句的 IF NOT EXISTS 是同一判据，不会打架。
	register(Migration{
		Version:   "596-sys-migration-marker",
		TableName: "sys_migration_marker",
		SQL:       mustSQL("596_sys_migration_marker.sql"),
	})

	// 597：一次性授权复制 + 落标记（同事务）。CheckSQL 查标记行，刻意不含 ? 占位符
	// （ApplyStatements 只在 SQL 里真出现占位符时才传表名）。
	register(Migration{
		Version:   "597-block-bulk-delete-grant-copy",
		TableName: "sys_migration_marker",
		CheckSQL: `SELECT CASE WHEN EXISTS (
    SELECT 1 FROM sys_migration_marker WHERE version = '597-block-bulk-delete-grant-copy'
) THEN 1 ELSE 0 END`,
		SQL: mustSQL("597_block_bulk_delete_grant_copy.sql"),
	})
}
