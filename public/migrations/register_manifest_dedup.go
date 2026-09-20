package migrations

import "sync"

// register_manifest_dedup.go — 产物表重复 manifest 列的收敛（迁移 305，审计 DB-02）。
//
// 与 register_build_lease.go 同一形态：由 register.go 的 init() 显式调用，
// 不在文件尾自注册。
func registerManifestDedup() {
	registerManifestDedupOnce.Do(registerManifestDedupSQL)
}

// registerManifestDedupOnce 让重复调用成为空操作（不会重复 register）。
var registerManifestDedupOnce sync.Once

// registerManifestDedupSQL 注册 305（真正干活的那一半，被 Once 包一层）。
func registerManifestDedupSQL() {
	// 判定「两件事都已完成」，而不是「表存在」：
	//   · presentation_artifacts 上已无 build_input_manifest 列；
	//   · page_artifacts 上不再有 NOT NULL 的 build_input_manifest 列
	//     （放开为 NULL 或整列都不在都算完成 —— 不能写成 is_nullable = 'YES'：
	//     列不存在时那个子查询返回空行，条件恒为假，迁移会每次启动重跑）。
	// 半途失败（A 成功 B 未落）时判定为「未完成」，整条 SQL 可重复执行。
	//
	// CheckSQL 里的 ? 由迁移器传入**表名**（这里是 TableName = presentation_artifacts），
	// 而且只传这一个参数 —— 另一张表名与两个列名必须写进 SQL 字面量，
	// 写第二个 ? 会直接报参数个数不匹配。
	register(Migration{
		Version:   "305-manifest-dedup",
		TableName: "presentation_artifacts",
		CheckSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM information_schema.columns " +
			" WHERE table_schema = current_schema() AND table_name = ? " +
			" AND column_name = 'build_input_manifest') = 0 " +
			"AND (SELECT COUNT(*) FROM information_schema.columns " +
			" WHERE table_schema = current_schema() AND table_name = 'page_artifacts' " +
			" AND column_name = 'build_input_manifest' AND is_nullable = 'NO') = 0 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("305_manifest_dedup.sql"),
	})
}
