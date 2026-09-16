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
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND deleted_at IS NULL AND title = '访问统计'",
		SQL:          mustSQL("149_analytics_menu.sql"),
	})

	// 150：文章管理后台菜单入口（INF-1，幂等 seed）。
	// 侧栏真源是代码配置（nav_menu.go 的「内容」组），本 seed 服务于后台「菜单管理」页 —— 见 SQL 顶部注释。
	registerSeed(Seed{
		Version:      "150-article-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND deleted_at IS NULL AND title = '文章'",
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
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND deleted_at IS NULL AND title = '客户管理'",
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
		SQL: mustSQL("165_orders_status_check.sql"),
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
		SQL: mustSQL("166_blocks_kind_check.sql"),
	})

	// 167 幂等判据取 orders 上的三条 trgm 索引：它们同时依赖 pg_trgm 扩展与这批建索引语句，
	// 三缺一即视为未应用（迁移本身全部 IF NOT EXISTS / DROP IF EXISTS，重跑安全）。
	register(Migration{
		Version:   "167-index-foundation",
		TableName: "orders",
		// schemaname 必须限定：pg_indexes 是全库视图，并行测试时别的隔离 schema 里的同名
		// 索引会被算进 COUNT，判定「已应用」后本条被静默跳过（该 schema 的 trgm 索引全缺）。
		CheckSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname LIKE 'idx_orders_%_trgm'",
		SQL:      mustSQL("167_index_foundation.sql"),
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
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'create_time'",
		SQL:      mustSQL("199_db019_db020_batch1.sql"),
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
	//
	// presentation 侧那一半用 to_regclass 而不是 'xxx'::regclass：迁移 207（CQ-015）
	// 会把那张表删掉，而 '缺表名'::regclass 在下次启动求值判定时会直接报错（启动失败）。
	// to_regclass 缺表返回 NULL，比较自然落空 —— 此时「表已不存在」本身就算这一半完成。
	register(Migration{
		Version:   "204-artifact-closure-fk-cascade",
		TableName: "page_artifact_objects",
		CheckSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM pg_constraint pc WHERE pc.conrelid = ?::regclass " +
			"AND pc.conname = 'page_artifact_objects_content_hash_fkey' " +
			"AND pg_get_constraintdef(pc.oid) LIKE '%ON DELETE CASCADE%') = 1 " +
			"AND (to_regclass('presentation_artifact_objects') IS NULL " +
			"OR (SELECT COUNT(*) FROM pg_constraint pc2 WHERE pc2.conrelid = to_regclass('presentation_artifact_objects') " +
			"AND pc2.conname = 'presentation_artifact_objects_content_hash_fkey' " +
			"AND pg_get_constraintdef(pc2.oid) LIKE '%ON DELETE CASCADE%') = 1) " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("204_artifact_closure_fk_cascade.sql"),
	})

	// 205：DB-019 全量收口 —— 剩余 46 张表的 created_at 与 33 张表的 updated_at
	// 统一改名（201 只做了 5 张叶子表的 created_at，且漏了它们的 updated_at）。
	// 判定按「全库不再存在 created_at / updated_at 列」而不是「表存在」：
	// 用默认判定会让整段 SQL 永不执行（同 201 踩过的坑）。
	// 判定里的旧列名**不能**跟着 Go 侧改名一起替换 —— 它检查的正是「旧名是否已消失」。
	//
	// TableName 取一个**哨兵名**而不是真实表：迁移器只给 CheckSQL 传一个参数（表名），
	// 而这条判定管的是全库、不针对单表 —— 护栏又要求每个自定义判定都接收那个参数。
	// 于是用 table_name <> ? 表达「除哨兵外全部」，哨兵不可能存在，条件等价于全库。
	// 若改成排除某张真实表（如 pages），那张表万一漏改，判定就会误报完成、迁移从此跳过。
	register(Migration{
		Version:   "205-db019-time-columns-full",
		TableName: "__db019_all_tables__",
		CheckSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() " +
			"AND column_name IN ('created_at', 'updated_at') AND table_name <> ?) = 0 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("205_db019_time_columns_full.sql"),
	})

	// 206：DB-019 连带修复 —— 触发器函数体里的旧列名。
	// RENAME COLUMN 只重写索引 / 视图 / 约束这类可解析对象，plpgsql 函数体是字符串，
	// 改名后仍按旧名解析（实测：采购收货链路整片失败）。
	// 判定按「函数定义里已无 updated_at」，而不是「函数是否存在」。
	// 判定里的 ? 接表名（护栏 TestCustomMigrationChecksAcceptTableParameter 要求每个自定义
	// 判定都接收它）：这里取该触发器真正操作的单头表，语义是「表在 且 函数已修好」——
	// 表都没了就不该算完成（那说明库被人手改过，启动时应当停下来而不是静默跳过）。
	register(Migration{
		Version:   "206-inventory-status-sync-fn-time-column",
		TableName: "inventory_purchase_orders",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NOT NULL AND (SELECT COUNT(*) FROM pg_proc p " +
			"JOIN pg_namespace n ON n.oid = p.pronamespace " +
			"WHERE n.nspname = current_schema() AND p.proname = 'fn_inventory_purchase_order_status_sync' " +
			"AND pg_get_functiondef(p.oid) LIKE '%update_time%') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("206_inventory_status_sync_fn_time_column.sql"),
	})

	// 207：CQ-015 —— 删除两张零消费方表（presentation_artifact_objects / publication_events）。
	// 判定表达「两张都不存在才算完成」：apply() 是 count > 0 即跳过，故取反。
	// 用 to_regclass 而不是 to_regclass(?) 单表判定 —— 这条迁移管的是两张表。
	register(Migration{
		Version:   "207-cq015-drop-unused-tables",
		TableName: "presentation_artifact_objects",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NULL AND to_regclass('publication_events') IS NULL " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("207_cq015_drop_unused_tables.sql"),
	})

	// 208：DB-020 软删除列名统一 —— sys_menus.deleted_time → deleted_at。
	// 判定按「旧列已消失」（不是「表存在」）：这条迁移做的就是改名，默认判定会误跳过。
	// 判定里的 'deleted_time' 字面量同样不能跟着 Go 侧 rename 一起替换。
	register(Migration{
		Version:   "208-db020-soft-delete-column",
		TableName: "sys_menus",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'deleted_time'",
		SQL: mustSQL("208_db020_soft_delete_column.sql"),
	})

	// 209：blocks.id 回到 uuid（主键选型判据：对外边界用不可枚举标识）。
	// 201 按「主键统一 bigint」把 blocks 划到自增侧，但它两条判据都踩：对外有 /api/block/*，
	// 且 props.blockId 写进 Page Document 并挂着 GIN 部分索引（idx_pages_blockref）。
	// 同批的 build_jobs / page_site_slots / publication_receipts / inventory_change_reasons
	// 是内部流水与字典，继续 bigint，不动。
	// 判定按「blocks.id 已是 uuid」：默认判定（表存在）会让这条迁移永不执行。
	register(Migration{
		Version:   "209-blocks-id-uuid",
		TableName: "blocks",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"AND column_name = 'id' AND data_type = 'uuid'",
		SQL: mustSQL("209_blocks_id_uuid.sql"),
	})

	// 210：pg_trgm 固定到专用 schema ext_shared（多 schema / 并发测试的前提）。
	// 扩展是**库级唯一**的，装进「当前 schema」会让并发的第二个 schema 静默跳过安装，
	// 随后建 trgm 索引报 operator class "gin_trgm_ops" does not exist。
	// 167/169/173 已带 WITH SCHEMA ext_shared（新库直接对），本迁移负责既有库：把扩展搬过来。
	register(Migration{
		Version:   "210-pg-trgm-shared-schema",
		TableName: "ext_shared",
		CheckSQL: "SELECT CASE WHEN to_regnamespace(?) IS NOT NULL AND EXISTS (" +
			"SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm' AND extnamespace = 'ext_shared'::regnamespace) " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("210_pg_trgm_shared_schema.sql"),
	})
}
