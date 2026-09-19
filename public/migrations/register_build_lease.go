package migrations

import "sync"

// register_build_lease.go — 构建队列的任务租约、来源互斥与完成归属（迁移 295）。
//
// 与 171（建 claim / 回收索引与 pending 去重键）是同一张表的先后两批形状：
// 171 让队列能被消费，295 让「认领 / 回收 / 完成」三件事各自有归属与互斥。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册）。
func registerBuildJobLease() {
	registerBuildJobLeaseOnce.Do(registerBuildJobLeaseSQL)
}

// registerBuildJobLeaseOnce 让重复调用成为空操作（不会重复 register）。
var registerBuildJobLeaseOnce sync.Once

// registerBuildJobLeaseSQL 注册 295（真正干活的那一半，被 Once 包一层）。
func registerBuildJobLeaseSQL() {
	// 判定「六列 + 来源互斥索引都在位」而不是「表存在」：build_jobs 从 init_builder_schema
	// 起就在，只看表名会让这条迁移对既有库永久跳过。
	// CheckSQL 里的 ? 由迁移器传入**表名**，而且只传这一个参数 —— 需要按对象名比较的
	// 条件（索引名）必须写进 SQL 字面量，写成第二个 ? 会让检查直接报参数个数不匹配。
	register(Migration{
		Version:   "295-build-jobs-lease",
		TableName: "build_jobs",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END " +
			"FROM information_schema.columns c " +
			"WHERE c.table_schema = current_schema() AND c.table_name = ? " +
			"AND c.column_name IN ('project_id', 'lang', 'intent', 'lease_token', 'lease_expires_time', 'attempt') " +
			"AND EXISTS (SELECT 1 FROM pg_indexes i WHERE i.schemaname = current_schema() " +
			"AND i.tablename = c.table_name AND i.indexname = 'uq_build_jobs_running_source')",
		SQL: mustSQL("295_build_jobs_lease.sql"),
	})
}
