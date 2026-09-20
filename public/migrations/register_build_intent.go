package migrations

import "sync"

// register_build_intent.go — 构建队列待办去重键的语言 / 意图维度（迁移 307，审计 ARCH-04）。
//
// 与 171（pending 去重键 = 来源 + 输入）、295（语言 / 意图 / 租约列）是同一张表的先后几批形状：
// 171 让队列能被消费，295 让任务的作用域显式入库，307 让**去重键**跟上这两个维度 ——
// 否则多语言任务会被误去重成一条、人工构建会被依赖重建吞掉。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册）。
func registerBuildJobIntent() {
	registerBuildJobIntentOnce.Do(registerBuildJobIntentSQL)
}

// registerBuildJobIntentOnce 让重复调用成为空操作（不会重复 register）。
var registerBuildJobIntentOnce sync.Once

// registerBuildJobIntentSQL 注册 307（真正干活的那一半，被 Once 包一层）。
func registerBuildJobIntentSQL() {
	// 判定「uq_build_jobs_pending 的唯一键正好是那五列、且仍是部分索引」而不是「表存在」：
	// 只看表名会让这条迁移对既有库永久跳过；只看列数会让「列在但键没换」也算通过。
	//
	// 按 pg_index / pg_attribute 读**键列**（而不是拿 pg_indexes.indexdef 做字符串匹配）：
	// indexdef 的列清单文本随 PG 版本变化，判据会被版本差异骗过去。
	// CheckSQL 里的 ? 由迁移器传入**表名**，且只传这一个参数（178 的坑）。
	register(Migration{
		Version:   "307-build-jobs-intent",
		TableName: "build_jobs",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 5 THEN 1 ELSE 0 END " +
			"FROM pg_index x " +
			"JOIN pg_class c ON c.oid = x.indrelid " +
			"JOIN pg_namespace n ON n.oid = c.relnamespace " +
			"JOIN pg_class ic ON ic.oid = x.indexrelid " +
			"JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY (x.indkey) " +
			"WHERE n.nspname = current_schema() AND c.relname = ? " +
			"AND ic.relname = 'uq_build_jobs_pending' " +
			"AND x.indisunique " +
			"AND x.indpred IS NOT NULL " +
			"AND a.attname IN ('source_type', 'source_id', 'build_input_hash', 'lang', 'intent')",
		SQL: mustSQL("307_build_jobs_intent.sql"),
	})
}
