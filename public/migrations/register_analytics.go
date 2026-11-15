package migrations

func registerAnalyticsSeoAndPermissions() {

	registerSeed(Seed{
		Version:      "148-analytics-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'analytics:view'",
		SQL:          mustSQL("148_analytics_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "149-analytics-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND deleted_at IS NULL AND title = '访问统计'",
		SQL:          mustSQL("149_analytics_menu.sql"),
	})

	registerSeed(Seed{
		Version:      "150-article-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND deleted_at IS NULL AND title = '文章'",
		SQL:          mustSQL("150_article_menu.sql"),
	})

	registerSeed(Seed{
		Version:   "151-missing-permission-points",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('page:delete', 'block:clone')) = 2 " +
			"AND (SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v3 IN ('page:delete', 'block:clone')) >= 2 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("151_missing_permission_points.sql"),
	})

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

	registerSeed(Seed{
		Version:   "154-presentation-update-url-permission",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'presentation:update_url') = 1 " +
			"AND (SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v3 = 'presentation:update_url') >= 1 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("154_presentation_update_url_permission.sql"),
	})

	register(Migration{
		Version:   "155b-presentation-i18n",
		TableName: "presentation_publications",
		SQL:       mustSQL("155_presentation_i18n.sql"),
	})

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
		Version:   "313-article-detail-template-props",
		TableName: "content_templates",

		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NULL THEN 1 " +
			"WHEN EXISTS (SELECT 1 FROM content_templates WHERE entity_type = 'article' " +
			"AND draft_document::text LIKE '%\"mode\": \"plain\"%') THEN 0 ELSE 1 END",
		SQL: mustSQL("313_article_detail_template_props.sql"),
	})

	register(Migration{
		Version:   "161-data-retention",
		TableName: "page_views_daily",
		SQL:       mustSQL("161_data_retention.sql"),
	})

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
		Version:   "223-content-template-default-per-project",
		TableName: "content_templates",
		CheckSQL: "SELECT CASE WHEN COUNT(*) > 0 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_content_templates_default_per_project_type'",
		SQL: mustSQL("223_content_template_default_per_project.sql"),
	})

	register(Migration{
		Version:   "165-orders-status-check",
		TableName: "orders",

		CheckSQL: "WITH target AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint WHERE conrelid = ?::regclass AND conname = 'orders_status_check') " +
			"SELECT CASE WHEN (SELECT COUNT(*) FROM pg_constraint pc JOIN target t ON t.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%refunded%') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("165_orders_status_check.sql"),
	})

	register(Migration{
		Version:   "166-blocks-kind-check",
		TableName: "blocks",

		CheckSQL: "WITH target AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint WHERE conrelid = ?::regclass AND conname = 'blocks_kind_check') " +
			"SELECT CASE WHEN (SELECT COUNT(*) FROM pg_constraint pc JOIN target t ON t.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%snippet%') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("166_blocks_kind_check.sql"),
	})

	register(Migration{
		Version:   "167-index-foundation",
		TableName: "orders",

		CheckSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname LIKE 'idx_orders_%_trgm'",
		SQL:      mustSQL("167_index_foundation.sql"),
	})

	register(Migration{
		Version:   "168-mail-campaign-event-totals",
		TableName: "mail_campaigns",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name IN ('open_count','click_count')",
		SQL:       mustSQL("168_mail_campaign_event_totals.sql"),
	})

	register(Migration{
		Version:   "169-contents-query-projection",
		TableName: "contents",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'title' AND is_generated = 'ALWAYS'",
		SQL:       mustSQL("169_contents_query_projection.sql"),
	})

	register(Migration{
		Version:   "170-analytics-rollup",
		TableName: "page_views_daily",

		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'scope'",
		SQL:      mustSQL("170_analytics_rollup.sql"),
	})

	register(Migration{
		Version:   "171-build-jobs-queue",
		TableName: "build_jobs",
		// 判定与 SQL 保持历史原文：idx_build_jobs_running 由 295 删除，claim 由
		// 764-build-jobs-claim-index 补建 —— 原文重跑会撞已改名的 created_at。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'uq_build_jobs_pending'",
		SQL:      mustSQL("171_build_jobs_queue.sql"),
	})

	register(Migration{
		Version:   "172-time-columns-to-timestamptz",
		TableName: "orders",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND data_type = 'timestamp without time zone' AND table_name <> ?",
		SQL:       mustSQL("172_time_columns_to_timestamptz.sql"),
	})

	register(Migration{
		Version:   "173-partition-append-only-tables",
		TableName: "page_views",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = current_schema() AND c.relname = ? AND c.relkind = 'p'",
		SQL:       mustSQL("173_partition_append_only_tables.sql"),
	})

	register(Migration{
		Version:   "174-site-slot-dependency-kind",
		TableName: "page_dependencies",
		CheckSQL:  "SELECT CASE WHEN COUNT(*) > 0 THEN 1 ELSE 0 END FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid JOIN pg_namespace n ON n.oid = t.relnamespace WHERE n.nspname = current_schema() AND t.relname = ? AND c.conname = 'page_dependencies_dependency_kind_check' AND (pg_get_constraintdef(c.oid) LIKE '%site_slot%' OR pg_get_constraintdef(c.oid) LIKE '%page_target%')",
		SQL:       mustSQL("174_site_slot_dependency_kind.sql"),
	})

	register(Migration{
		Version:   "175-archive-presentation-role",
		TableName: "presentation_instances",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'instance_role'",
		SQL:       mustSQL("175_archive_presentation_role.sql"),
	})

	register(Migration{
		Version:   "199b-webhook",
		TableName: "webhook_endpoints",
		CheckSQL:  "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL:       mustSQL("199_webhook.sql"),
	})

	register(Migration{
		Version:   "201-db019-db020-batch1",
		TableName: "build_jobs",

		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'create_time'",
		SQL:      mustSQL("201_db019_db020_batch1.sql"),
	})

	register(Migration{
		Version:   "202-inventory-warehouse-status-check",
		TableName: "inventory_warehouses",
		CheckSQL: "WITH target AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint WHERE conrelid = ?::regclass AND conname = 'inventory_warehouses_status_check') " +
			"SELECT CASE WHEN (SELECT COUNT(*) FROM pg_constraint pc JOIN target t ON t.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%disabled%') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("202_inventory_warehouse_status_check.sql"),
	})

	register(Migration{
		Version:   "203-drop-unused-user-tables",
		TableName: "user_sessions",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NULL AND to_regclass('user_meta') IS NULL " +
			"AND to_regclass('user_app_passwords') IS NULL AND to_regclass('user_oauth_bindings') IS NULL THEN 1 ELSE 0 END",
		SQL: mustSQL("203_drop_unused_user_tables.sql"),
	})

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

	register(Migration{
		Version:   "205-db019-time-columns-full",
		TableName: "__db019_all_tables__",
		CheckSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() " +
			"AND column_name IN ('created_at', 'updated_at') AND table_name <> ?) = 0 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("205_db019_time_columns_full.sql"),
	})

	register(Migration{
		Version:   "206-inventory-status-sync-fn-time-column",
		TableName: "inventory_purchase_orders",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NOT NULL AND (SELECT COUNT(*) FROM pg_proc p " +
			"JOIN pg_namespace n ON n.oid = p.pronamespace " +
			"WHERE n.nspname = current_schema() AND p.proname = 'fn_inventory_purchase_order_status_sync' " +
			"AND pg_get_functiondef(p.oid) LIKE '%update_time%') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("206_inventory_status_sync_fn_time_column.sql"),
	})

	register(Migration{
		Version:   "207-cq015-drop-unused-tables",
		TableName: "presentation_artifact_objects",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NULL AND to_regclass('publication_events') IS NULL " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("207_cq015_drop_unused_tables.sql"),
	})

	register(Migration{
		Version:   "208-db020-soft-delete-column",
		TableName: "sys_menus",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'deleted_time'",
		SQL: mustSQL("208_db020_soft_delete_column.sql"),
	})

	register(Migration{
		Version:   "209-blocks-id-uuid",
		TableName: "blocks",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"AND column_name = 'id' AND data_type = 'uuid'",
		SQL: mustSQL("209_blocks_id_uuid.sql"),
	})

	register(Migration{
		Version:   "210-pg-trgm-shared-schema",
		TableName: "ext_shared",

		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM pg_extension e " +
			"JOIN pg_namespace n ON n.oid = e.extnamespace " +
			"WHERE e.extname = 'pg_trgm' AND n.nspname = ?) THEN 1 ELSE 0 END",
		SQL: mustSQL("210_pg_trgm_shared_schema.sql"),
	})

	register(Migration{
		Version:   "211-drop-block-id-uuid-map",
		TableName: "block_id_uuid_map",
		CheckSQL:  "SELECT CASE WHEN to_regclass(?) IS NULL THEN 1 ELSE 0 END",
		SQL:       mustSQL("211_drop_block_id_uuid_map.sql"),
	})

	register(Migration{
		Version:   "212-webhook-time-columns",
		TableName: "webhook_endpoints",

		CheckSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name IN (?, 'webhook_deliveries') " +
			"AND column_name IN ('create_time', 'update_time') AND data_type = 'timestamp with time zone'",
		SQL: mustSQL("212_webhook_time_columns.sql"),
	})

	registerSeed(Seed{
		Version:      "213-webhook-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code LIKE 'webhook:%'",
		SQL:          mustSQL("213_webhook_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "214-missing-permission-points-2",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 5 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN ('build:jobs', 'build:queue', 'build:retry', 'publication:seo_audit', 'order:coupon_count_audit')",
		SQL:          mustSQL("214_missing_permission_points_2.sql"),
	})

	register(Migration{
		Version:   "215-project-isolation-rls",
		TableName: "orders",
		CheckSQL: "SELECT CASE WHEN COUNT(*) > 0 THEN 1 ELSE 0 END FROM pg_policies " +
			"WHERE schemaname = current_schema() AND tablename = ? AND policyname = 'project_isolation'",
		SQL: mustSQL("215_project_isolation_rls.sql"),
	})
}
