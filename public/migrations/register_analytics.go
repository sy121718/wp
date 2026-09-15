package migrations

// registerAnalyticsSeoAndPermissions 注册「统计、SEO 与权限点补全（147–175）」。
//
// 由 register.go 的 init() 按主题拆出：本文件只做注册，不含任何 SQL 文本，
// 注册项引用的 SQL 一律经 mustSQL 从嵌入的 .sql 取。
func registerAnalyticsSeoAndPermissions() {
	// 148：访问统计权限点 + 超管策略（1 个只读权限点存在才算已 seed）。
	registerSeed(Seed{
		Version:      "148-analytics-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'analytics:view'",
		SQL:          mustSQL("148_analytics_permissions.sql"),
	})

	// 149：访问统计后台菜单入口（幂等 seed）。
	registerSeed(Seed{
		Version:      "149-analytics-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND deleted_time IS NULL AND title = '访问统计'",
		SQL:          mustSQL("149_analytics_menu.sql"),
	})

	// 150：文章管理后台菜单入口（INF-1，幂等 seed）。
	// 侧栏真源是代码配置（nav_menu.go 的「内容」组），本 seed 服务于后台「菜单管理」页 —— 见 SQL 顶部注释。
	registerSeed(Seed{
		Version:      "150-article-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND deleted_time IS NULL AND title = '文章'",
		SQL:          mustSQL("150_article_menu.sql"),
	})

	// 151：补齐「有路由、无权限点」的接口（删页面 / 区块克隆）。
	//
	// 跳过条件必须同时看**两张表**：权限点齐了但策略没齐时仍要执行 ——
	// 只查权限点会让"权限点先落库、策略后补"的那一半永远补不上（051/079 踩过这个坑）。
	registerSeed(Seed{
		Version:   "151-missing-permission-points",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('page:delete', 'block:clone')) = 2 " +
			"AND (SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v3 IN ('page:delete', 'block:clone')) >= 2 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("151_missing_permission_points.sql"),
	})

	// 152：后台客户管理权限点（4 条）+ 超管策略。
	//
	// 跳过条件同时看两张表（与 151 同因）：4 条权限点齐了、且超管策略至少各有 1 行，
	// 才算这条 seed 已完成 —— 只查权限点会留下「权限点有了、策略没补」的空窗，
	// 而那种状态下超管点客户页的按钮就是 403。
	registerSeed(Seed{
		Version:   "152-customer-admin-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_permission WHERE permission_code IN " +
			"('user:customer_list', 'user:customer_detail', 'user:customer_status', 'user:customer_unlock')) = 4 " +
			"AND (SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v3 IN " +
			"('user:customer_list', 'user:customer_detail', 'user:customer_status', 'user:customer_unlock')) >= 4 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("152_customer_admin_permissions.sql"),
	})

	// 153：客户管理后台菜单入口（幂等 seed）。
	// 侧栏真源是代码配置（nav_menu.go 的「系统」组），本 seed 服务于后台「菜单管理」页。
	registerSeed(Seed{
		Version:      "153-customer-admin-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND deleted_time IS NULL AND title = '客户管理'",
		SQL:          mustSQL("153_customer_admin_menu.sql"),
	})

	// 154：详情页改 URL 权限点（1 条）+ 超管策略。
	//
	// 跳过条件同时看两张表（与 151/152 同因）：权限点齐了但策略没齐时仍要执行 ——
	// 只查权限点会留下「权限点有了、策略没补」的空窗，那种状态下超管点
	// 「改 URL」就是 403。
	registerSeed(Seed{
		Version:   "154-presentation-update-url-permission",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'presentation:update_url') = 1 " +
			"AND (SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v3 = 'presentation:update_url') >= 1 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("154_presentation_update_url_permission.sql"),
	})

	// 155：presentation 多语言产物（I18N-013）。
	register(Migration{
		Version:   "155-presentation-i18n",
		TableName: "presentation_publications",
		SQL:       mustSQL("155_presentation_i18n.sql"),
	})
	// presentation_artifacts.lang 列（同批迁移，按列存在判定避免误跳过）。
	register(Migration{
		Version:   "155-presentation-artifacts-lang",
		TableName: "presentation_artifacts",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'lang'",
		SQL:       mustSQL("155_presentation_i18n.sql"),
	})
	registerSeed(Seed{
		Version:      "156-i18n-seed-fragments",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'site.fragment.cart.empty' AND lang = 'zh-CN'",
		SQL:          mustSQL("156_i18n_seed_fragments.sql"),
	})
	registerSeed(Seed{
		Version:      "157-i18n-seed-fragments-user-orders",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'site.fragment.common.session_not_ready' AND lang = 'zh-CN'",
		SQL:          mustSQL("157_i18n_seed_fragments_user_orders.sql"),
	})
	registerSeed(Seed{
		Version:      "158-i18n-seed-fragments-bundle-jet",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'site.fragment.bundle.qty_aria' AND lang = 'zh-CN'",
		SQL:          mustSQL("158_i18n_seed_fragments_bundle_jet.sql"),
	})

	// 159：默认文章详情内容模板（EDT-002）。
	registerSeed(Seed{
		Version:      "159-article-detail-template",
		TableName:    "content_templates",
		ConditionSQL: "SELECT COUNT(*) FROM content_templates WHERE entity_type = 'article'",
		SQL:          mustSQL("159_article_detail_template.sql"),
	})

	register(Migration{
		Version:   "160-presentation-entity-types",
		TableName: "presentation_instances",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM pg_constraint WHERE conrelid = ?::regclass AND conname = 'presentation_instances_entity_type_check'",
		SQL:       mustSQL("160_presentation_entity_types.sql"),
	})

	register(Migration{
		Version:   "161-data-retention",
		TableName: "page_views_daily",
		SQL:       mustSQL("161_data_retention.sql"),
	})

	// 162：库存流水按仓库 + 时间索引（IDX-007）。
	register(Migration{
		Version:   "162-inventory-movements-wh-time-index",
		TableName: "inventory_stock_movements",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_inventory_movements_wh_time'",
		SQL: mustSQL("162_inventory_movements_wh_time_index.sql"),
	})

	registerSeed(Seed{
		Version:      "163-i18n-seed-dashboard-titles",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'MsgArticlesTitle' AND lang = 'zh-CN'",
		SQL:          mustSQL("163_i18n_seed_dashboard_titles.sql"),
	})

	register(Migration{
		Version:   "164-content-template-is-default",
		TableName: "content_templates",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'is_default'",
		SQL:       mustSQL("164_content_template_is_default.sql"),
	})

	register(Migration{
		Version:   "165-orders-status-check",
		TableName: "orders",
		// apply() 是「count > 0 → 跳过」，而本迁移 SQL 做的是「DROP + ADD 约束」，
		// 所以判定必须表达「约束定义已是新版」：原写法 COUNT(=0)=0→1 把「约束不存在
		// （正需要加）」判成已完成，orders.status 的 DDL CHECK 从未落地。
		// 与 071 同因：pg_get_constraintdef 只对本迁移物化出的 OID 求值，避免与
		// 并发 DROP SCHEMA 的其它测试包争用「打开关系」。
		CheckSQL: "WITH target AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint WHERE conrelid = ?::regclass AND conname = 'orders_status_check') " +
			"SELECT CASE WHEN (SELECT COUNT(*) FROM pg_constraint pc JOIN target t ON t.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%refunded%') = 1 THEN 1 ELSE 0 END",
		SQL:       mustSQL("165_orders_status_check.sql"),
	})

	register(Migration{
		Version:   "166-blocks-kind-check",
		TableName: "blocks",
		// 只判「约束存在」区分不了 kind 集合扩展前后（旧集合同样存在），
		// 故按定义里的新 kind 判定；物化 OID 的理由同 071。
		CheckSQL: "WITH target AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint WHERE conrelid = ?::regclass AND conname = 'blocks_kind_check') " +
			"SELECT CASE WHEN (SELECT COUNT(*) FROM pg_constraint pc JOIN target t ON t.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%snippet%') = 1 THEN 1 ELSE 0 END",
		SQL:       mustSQL("166_blocks_kind_check.sql"),
	})

	// 167 幂等判据取 orders 上的三条 trgm 索引：它们同时依赖 pg_trgm 扩展与这批建索引语句，
	// 三缺一即视为未应用（迁移本身全部 IF NOT EXISTS / DROP IF EXISTS，重跑安全）。
	register(Migration{
		Version:   "167-index-foundation",
		TableName: "orders",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM pg_indexes WHERE tablename = ? AND indexname LIKE 'idx_orders_%_trgm'",
		SQL:       mustSQL("167_index_foundation.sql"),
	})

	register(Migration{
		Version:   "168-mail-campaign-event-totals",
		TableName: "mail_campaigns",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name IN ('open_count','click_count')",
		SQL:       mustSQL("168_mail_campaign_event_totals.sql"),
	})

	// 判定条件是「生成列已存在」而不是「列存在」：DB-024 的收益全在「写入无需改动」，
	// 普通列做不到这一点，判定必须能把两者区分开。
	register(Migration{
		Version:   "169-contents-query-projection",
		TableName: "contents",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'title' AND is_generated = 'ALWAYS'",
		SQL:       mustSQL("169_contents_query_projection.sql"),
	})

	register(Migration{
		Version:   "170-analytics-rollup",
		TableName: "page_views_daily",
		// 判定「已扩到汇总口径」而不是「表存在」：161 早就建了这张表且零消费，
		// 只看表名会让本迁移被跳过，而 scope 列才是它能否承担汇总职责的关键。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'scope'",
		SQL:      mustSQL("170_analytics_rollup.sql"),
	})

	register(Migration{
		Version:   "171-build-jobs-queue",
		TableName: "build_jobs",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'uq_build_jobs_pending'",
		SQL:       mustSQL("171_build_jobs_queue.sql"),
	})

	// CheckSQL 的 ? 接表名（必须参数化）；判定条件是「全库已无 timestamp without time zone 列」——
	// 逐表判定会被表名参数限制在一张表上，而这条迁移管的是全库。
	register(Migration{
		Version:   "172-time-columns-to-timestamptz",
		TableName: "orders",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND data_type = 'timestamp without time zone' AND table_name <> ?",
		SQL:       mustSQL("172_time_columns_to_timestamptz.sql"),
	})

	// 判定「已是分区表」（relkind='p'）而不是「表存在」：迁移前这三张表本来就存在。
	register(Migration{
		Version:   "173-partition-append-only-tables",
		TableName: "page_views",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = current_schema() AND c.relname = ? AND c.relkind = 'p'",
		SQL:       mustSQL("173_partition_append_only_tables.sql"),
	})

	// 判定约束定义里是否已包含 site_slot：只看「约束存在」无法区分扩展前后。
	register(Migration{
		Version:   "174-site-slot-dependency-kind",
		TableName: "page_dependencies",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) > 0 THEN 1 ELSE 0 END FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid JOIN pg_namespace n ON n.oid = t.relnamespace WHERE n.nspname = current_schema() AND t.relname = ? AND c.conname = 'page_dependencies_dependency_kind_check' AND pg_get_constraintdef(c.oid) LIKE '%site_slot%'",
		SQL:       mustSQL("174_site_slot_dependency_kind.sql"),
	})

	// 判定角色列是否已存在：只看表存在无法区分迁移前后。
	register(Migration{
		Version:   "175-archive-presentation-role",
		TableName: "presentation_instances",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'instance_role'",
		SQL:       mustSQL("175_archive_presentation_role.sql"),
	})

	// 199：webhook 外部集成通道（OSS-006 + SEC-015）。
	register(Migration{
		Version:   "199-webhook",
		TableName: "webhook_endpoints",
		CheckSQL:  "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL:       mustSQL("199_webhook.sql"),
	})

	// 201：DB-019/020 第一批（build_jobs 等五表 id→bigint identity + created_at→create_time）。
	register(Migration{
		Version:   "201-db019-db020-batch1",
		TableName: "build_jobs",
		// 必须按「目标列已存在」判定：build_jobs 由 init_builder_schema.sql 先建，
		// 默认判据（表存在即跳过）会让整批 SQL 永不执行。
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'create_time'",
		SQL:       mustSQL("199_db019_db020_batch1.sql"),
	})

	// 202：DB-015 残余第三处 —— inventory_warehouses.status 的 DDL CHECK。
	// 判定按「约束定义已是新版」而不是「约束存在」，理由同 165（SQL 是 DROP + ADD）。
	register(Migration{
		Version:   "202-inventory-warehouse-status-check",
		TableName: "inventory_warehouses",
		CheckSQL: "WITH target AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint WHERE conrelid = ?::regclass AND conname = 'inventory_warehouses_status_check') " +
			"SELECT CASE WHEN (SELECT COUNT(*) FROM pg_constraint pc JOIN target t ON t.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%disabled%') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("202_inventory_warehouse_status_check.sql"),
	})

	// 203：删除开发阶段用不上的 user 表（会话台账 + 三张预留给将来的空表）。
	// 判据表达的是「四张表都不存在才算完成」—— apply() 是 count > 0 即跳过，故如此取反。
	register(Migration{
		Version:   "203-drop-unused-user-tables",
		TableName: "user_sessions",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NULL AND to_regclass('user_meta') IS NULL " +
			"AND to_regclass('user_app_passwords') IS NULL AND to_regclass('user_oauth_bindings') IS NULL THEN 1 ELSE 0 END",
		SQL: mustSQL("203_drop_unused_user_tables.sql"),
	})

	// 204：内容对象闭包外键补 ON DELETE CASCADE（否则内容对象 GC 被外键挡下、静默泄漏）。
	// 判定按约束定义，并用 MATERIALIZED CTE 先锁定 OID 再 deparse（理由同 071）。
	register(Migration{
		Version:   "204-artifact-closure-fk-cascade",
		TableName: "page_artifact_objects",
		CheckSQL: "WITH target AS MATERIALIZED (SELECT oid FROM pg_constraint " +
			"WHERE conname IN ('page_artifact_objects_content_hash_fkey', 'presentation_artifact_objects_content_hash_fkey') " +
			"AND conrelid IN (?::regclass, 'presentation_artifact_objects'::regclass)) " +
			"SELECT CASE WHEN (SELECT COUNT(*) FROM pg_constraint pc JOIN target t ON t.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%ON DELETE CASCADE%') = 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("204_artifact_closure_fk_cascade.sql"),
	})
}
