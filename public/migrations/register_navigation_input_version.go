package migrations

// register_navigation_input_version.go — 实体输入代次（迁移 699）。
//
// 与 698 是同一批的两侧：698 给 build_jobs 补「任务序列号」，699 给**实体**补
// 「编译输入代次」（pages / presentation_instances）。二者语义不同、不互相赋值：
// 任务表会被 retention 清理、号会倒退；实体版本只由输入变更的同一个事务原子 +1。
//
// 判定对象是「三件形状都在位」而不是「表存在」：
//   - pages / presentation_instances 从建库起就在，只看表名会让这条迁移对既有库永久跳过；
//   - 只判其中一列会让「跑了一半」的库被误判为已完成（另一侧的激活谓词随后静默失效）；
//   - receipt_state 的 CHECK 必须真的含 superseded，否则收敛例程写该终态时当场被拒。
//
// CheckSQL 里的 ? 由迁移器传入**表名**，且只传这一个参数 —— 需要按对象名比较的条件
// （列名、约束名）一律写进 SQL 字面量。本迁移跨两张表，TableName 取第一张，
// 其余两张按**固定名字**在 SQL 里直接判（不能用 ?，迁移器只绑定同一个值三次）。
func init() {
	register(Migration{
		Version:   "699-navigation-input-version",
		TableName: "pages",
		// 三件判据（任一缺失 COUNT(*) 就 < 3 → 迁移重跑，SQL 本身幂等）：
		//   ① pages.input_version 存在且是 bigint；
		//   ② presentation_instances.input_version 存在且是 bigint；
		//   ③ publication_receipts_receipt_state_check 的定义里含 superseded。
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
