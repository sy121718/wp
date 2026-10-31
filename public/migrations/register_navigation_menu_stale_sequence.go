package migrations

// register_navigation_menu_stale_sequence.go — menu_stale 可靠入队的队列侧形状（迁移 698）。
//
// 与 171 / 295 / 307 是 build_jobs 的先后几批形状：171 让队列能被消费，295 把作用域与
// 租约显式化，307 让去重键跟上语言 / 意图，698 补上「菜单变化的版本号源」、
// 「来源能表达工程 + 位置」（source_id 放宽为 text）、「同一语境同一版本的去重键」。
// 本文件与 register_seo_audit_i18n.go 同形：文件自带 init，不在 register.go 的 init() 里
// 挂调用（那里挂会让同一版本被注册两次）。
//
// 判定对象是「整套形状都在位」而不是「表存在」：
//   - build_jobs 从 init_builder_schema 起就在，只看表名会让这条迁移对既有库永久跳过；
//   - 列 / 索引在但列型还是 uuid（手工跑过半条、或有人把列改回去）同样必须重跑；
//     只看列名会让「source_id 仍是 uuid」的库被误判为已完成，而 menu_stale 插入会当场失败；
//   - 只判其中一件会让「跑了一半」的库被判成已完成。号源表（build_job_seq）尤其不能漏：
//     它一旦不在，入队取号当场失败，而其余六件都在的库会被判成「已完成」永久跳过。
//
// CheckSQL 里的 ? 由迁移器传入**表名**，且只传这一个参数 —— 需要按对象名比较的条件
// （列名、索引名、约束名）一律写进 SQL 字面量。

func init() {
	register(Migration{
		Version:   "698-navigation-menu-stale-sequence",
		TableName: "build_jobs",
		//
		// 七件判据（任一缺失或缺件 → CASE 落到 0 → 迁移重跑，SQL 本身幂等）：
		//   ① build_jobs.source_id 的列型是 text（不是 uuid / 不是缺列）；
		//   ② build_jobs 有 input_version 列且是 bigint；
		//   ③ build_jobs 有 context_key 列且是 text；
		//   ④ uq_build_jobs_context_version 在位、唯一、带谓词，键列正好是
		//      (context_key, input_version) 两列；
		//   ⑤ build_jobs_source_type_check 定义里含 menu_stale；
		//   ⑥ build_jobs_menu_stale_shape_check 在位，且定义里含 menu_stale 与 context_key
		//      （只查名字会被「约束在但表达式被改宽」骗过去）；
		//   ⑦ build_job_seq 表存在、ck_build_job_seq_kind 在位、RLS 已 ENABLE 且 FORCE、
		//      策略 project_isolation 存在 —— 号源表是整套机制的根，任一缺失都意味着
		//      「表在、机制不在」。
		//
		// 为什么用逐件 CASE 而不是 COUNT(*) = 7：UNION ALL 计数会把「同一件满足两次」
		// 与「另一件缺失」算成同一个数，逐件判存在与否才贴近「都在位」这句话。
		// 占位符约定：迁移器只传**一个**表名实参，因此本 CheckSQL 不含 `?`，对象名一律
		// 写进 SQL 字面量（与迁移器单参数调用约定一致）。
		// 索引键列用 pg_attribute 读（与 register_build_intent.go 同口径），不拿
		// pg_indexes.indexdef 的文本做匹配 —— 那段文本随 PG 版本变。
		CheckSQL: `SELECT CASE WHEN (
			(SELECT COUNT(*) FROM information_schema.columns c
			  WHERE c.table_schema = current_schema() AND c.table_name = 'build_jobs'
			    AND c.column_name = 'source_id' AND c.data_type = 'text') = 1
			AND (SELECT COUNT(*) FROM information_schema.columns c
			  WHERE c.table_schema = current_schema() AND c.table_name = 'build_jobs'
			    AND c.column_name = 'input_version' AND c.data_type = 'bigint') = 1
			AND (SELECT COUNT(*) FROM information_schema.columns c
			  WHERE c.table_schema = current_schema() AND c.table_name = 'build_jobs'
			    AND c.column_name = 'context_key' AND c.data_type = 'text') = 1
			AND (SELECT COUNT(DISTINCT a.attname) FROM pg_index x
			        JOIN pg_class c ON c.oid = x.indrelid
			        JOIN pg_class ic ON ic.oid = x.indexrelid
			        JOIN pg_namespace n ON n.oid = c.relnamespace
			        JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY (x.indkey)
			        WHERE n.nspname = current_schema() AND c.relname = 'build_jobs'
			          AND ic.relname = 'uq_build_jobs_context_version'
			          AND x.indisunique AND x.indpred IS NOT NULL
			          AND x.indnkeyatts = 2
			          AND a.attname IN ('context_key', 'input_version')) = 2
			AND (SELECT COUNT(*) FROM pg_constraint k
			      JOIN pg_class c ON c.oid = k.conrelid
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_jobs'
			        AND k.conname = 'build_jobs_source_type_check'
			        AND pg_get_constraintdef(k.oid) LIKE '%menu_stale%') = 1
			AND (SELECT COUNT(*) FROM pg_constraint k
			      JOIN pg_class c ON c.oid = k.conrelid
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_jobs'
			        AND k.conname = 'build_jobs_menu_stale_shape_check'
			        AND k.contype = 'c'
			        AND pg_get_constraintdef(k.oid) LIKE '%menu_stale%'
			        AND pg_get_constraintdef(k.oid) LIKE '%context_key%') = 1
			AND to_regclass(current_schema() || '.build_job_seq') IS NOT NULL
			AND (SELECT COUNT(*) FROM pg_constraint k
			      JOIN pg_class c ON c.oid = k.conrelid
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_job_seq'
			        AND k.conname = 'ck_build_job_seq_kind' AND k.contype = 'c') = 1
			AND (SELECT COUNT(*) FROM pg_class c
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_job_seq'
			        AND c.relrowsecurity AND c.relforcerowsecurity) = 1
			AND (SELECT COUNT(*) FROM pg_policy p
			      JOIN pg_class c ON c.oid = p.polrelid
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_job_seq'
			        AND p.polname = 'project_isolation') = 1
		) THEN 1 ELSE 0 END`,
		SQL: mustSQL("698_navigation_menu_stale_sequence.sql"),
	})
}
