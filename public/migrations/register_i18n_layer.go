package migrations

func registerI18nDataLayer() {

	register(Migration{
		Version:   "055-i18n-columns",
		TableName: "sys_i18n",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'category'",
		SQL:       mustSQL("055_i18n_columns.sql"),
	})

	register(Migration{
		Version:   "056-i18n-revision",
		TableName: "sys_i18n_revision",
		SQL:       mustSQL("056_i18n_revision.sql"),
	})

	register(Migration{
		Version:   "057-sys-menus-title-key",
		TableName: "sys_menus",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'title_key'",
		SQL:       mustSQL("057_sys_menus_title_key.sql"),
	})

	registerSeed(Seed{
		Version:      "058-i18n-seed-enums",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 199 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN'",
		SQL:          mustSQL("058_i18n_seed_enums.sql"),
	})

	registerSeed(Seed{
		Version:      "059-i18n-seed-shell",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 25 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key LIKE 'shell.%'",
		SQL:          mustSQL("059_i18n_seed_shell.sql"),
	})

	registerSeed(Seed{
		Version:   "060-i18n-seed-site-components",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 13 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND (" +
			"item_key LIKE 'site.component.gallery.%' OR item_key LIKE 'site.component.slider.%' OR " +
			"item_key LIKE 'site.component.nav.%' OR item_key LIKE 'site.component.video.%' OR " +
			"item_key LIKE 'site.component.countdown.%' OR item_key LIKE 'site.component.form.%' OR " +
			"item_key LIKE 'site.component.rating.%')",
		SQL: mustSQL("060_i18n_seed_site_components.sql"),
	})

	register(Migration{
		Version:   "061-page-artifacts-lang",
		TableName: "page_artifacts",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'lang'",
		SQL:       mustSQL("061_page_artifacts_lang.sql"),
	})

	register(Migration{
		Version:   "062-page-publications",
		TableName: "page_publications",
		SQL:       mustSQL("062_page_publications.sql"),
	})

	register(Migration{
		Version:   "063-page-stagings",
		TableName: "page_stagings",
		SQL:       mustSQL("063_page_stagings.sql"),
	})

	register(Migration{
		Version:   "064-project-locales",
		TableName: "project_locales",
		SQL:       mustSQL("064_project_locales.sql"),
	})

	registerSeed(Seed{
		Version:      "065-i18n-seed-language-switcher",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key LIKE 'site.component.languages.%'",
		SQL:          mustSQL("065_i18n_seed_language_switcher.sql"),
	})

	register(Migration{
		Version:   "066-sys-translation",
		TableName: "sys_translation",
		SQL:       mustSQL("066_sys_translation.sql"),
	})

	register(Migration{
		Version:   "067-media-center",
		TableName: "sys_attachment",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'generation'",
		SQL:       mustSQL("067_media_center.sql"),
	})

	register(Migration{
		Version:   "068-pg-jsonb-partial-index",
		TableName: "pages",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'idx_pages_blockref'",
		SQL:       mustSQL("068_pg_jsonb_partial_index.sql"),
	})

	register(Migration{
		Version:   "069-drop-obsolete-design-tables",
		TableName: "media_asset",
		CheckSQL: "SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() " +
			"AND table_name IN (?, 'media_asset_variant', 'media_reference', 'global_components', " +
			"'global_component_versions', 'global_component_policies')) = 0 " +
			"AND (SELECT COUNT(*) FROM pg_constraint WHERE conname IN (" +
			"'page_component_pins_component_id_fkey', 'page_component_pins_pinned_version_id_fkey', " +
			"'content_template_component_pins_component_id_fkey', 'content_template_component_pins_pinned_version_id_fkey'" +
			")) = 0 THEN 1 ELSE 0 END",
		SQL: mustSQL("069_drop_obsolete_design_tables.sql"),
	})

	register(Migration{
		Version:   "070-sys-status-index-audit",
		TableName: "sys_attachment",

		CheckSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() " +
			"AND indexname IN ('idx_att_file_path_alive', 'idx_mva_file_path', 'idx_att_cat_time_alive') " +
			"AND tablename IN (?, 'sys_media_variant')",
		SQL: mustSQL("070_sys_status_index_audit.sql"),
	})

	register(Migration{
		Version:   "071-dependency-fanout",
		TableName: "page_dependencies",

		CheckSQL: "WITH target_constraints AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint " +
			"WHERE conrelid IN ('page_dependencies'::regclass, 'presentation_dependencies'::regclass) " +
			"AND conname IN ('page_dependencies_dependency_kind_check', 'presentation_dependencies_dependency_kind_check')) " +
			"SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
			"AND indexname IN ('idx_page_deps_lookup', 'idx_pres_deps_lookup') " +
			"AND tablename IN (?, 'presentation_dependencies')) = 2 " +
			"AND (SELECT COUNT(*) FROM pg_constraint pc JOIN target_constraints tc ON tc.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%i18n%') = 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("071_dependency_fanout.sql"),
	})

	register(Migration{
		Version:   "199-rls-project-locales",
		TableName: "project_locales",
		CheckSQL:  "SELECT COUNT(*) FROM pg_policies WHERE schemaname = current_schema() AND tablename = ? AND policyname = 'project_isolation'",
		SQL:       mustSQL("199_rls_project_locales.sql"),
	})

	registerSeed(Seed{
		Version:      "228-i18n-seed-page-titles",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('MsgContentTemplatesTitle','MsgInventorySourcesTitle','MsgInventoryPurchasesTitle','MsgMasterDataChangesTitle')",
		SQL:          mustSQL("228_i18n_seed_page_titles.sql"),
	})

	registerSeed(Seed{
		Version:      "229-i18n-seed-admin-page-shell",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 13 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.common.action.reset','admin.common.field.project','admin.customers.help.label','admin.customers.list.empty_heading','admin.i18n.col.actions','admin.i18n.help.label','admin.i18n.list.empty_desc','admin.i18n.list.empty_title','admin.product_pricing.help.label','admin.product_pricing.history.note','admin.product_pricing.history.noteTail','admin.product_pricing.rules.note','admin.returns.help.label')",
		SQL:          mustSQL("229_i18n_seed_admin_page_shell.sql"),
	})

	registerSeed(Seed{
		Version:      "230-i18n-seed-list-skeleton",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.common.bulk.selectAll','admin.dashboard.stat.projects','admin.product_categories.empty.title')",
		SQL:          mustSQL("230_i18n_seed_list_skeleton.sql"),
	})

	registerSeed(Seed{
		Version:      "231-i18n-seed-list-skeleton-two",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.common.help.label','admin.masterdata.view.records','admin.masterdata.label.range')",
		SQL:          mustSQL("231_i18n_seed_list_skeleton_two.sql"),
	})

	registerSeed(Seed{
		Version:      "232-i18n-seed-admin-pages-round4",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.products.col.name','admin.inventory.change.actionLabel','admin.mail.marketing.status.hint')",
		SQL:          mustSQL("232_i18n_seed_admin_pages_round4.sql"),
	})

	registerSeed(Seed{
		Version:      "233-i18n-seed-product-detail",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.product_detail.title','admin.products.col.categories','admin.products.row.detail')",
		SQL:          mustSQL("233_i18n_seed_product_detail.sql"),
	})

	registerSeed(Seed{
		Version:      "235-i18n-seed-dashboard-degrade",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.dashboard.loadError'",
		SQL:          mustSQL("235_i18n_seed_dashboard_degrade.sql"),
	})

	registerSeed(Seed{
		Version:      "237-i18n-seed-block-project-required",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrBlockProjectRequired'",
		SQL:          mustSQL("237_i18n_seed_block_project_required.sql"),
	})

	registerSeed(Seed{
		Version:      "253-i18n-seed-product-variant-list",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrVariantSKUEmpty'",
		SQL:          mustSQL("253_product_variant_list_i18n.sql"),
	})

	registerSeed(Seed{
		Version:      "264-i18n-seed-product-warehouse-stock",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrContainerSKUTaken'",
		SQL:          mustSQL("264_product_warehouse_stock_i18n.sql"),
	})

	registerSeed(Seed{
		Version:      "258-i18n-seed-publication-audit-failed",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrAuditFailed'",
		SQL:          mustSQL("258_publication_audit_failed_i18n.sql"),
	})

	registerSeed(Seed{
		Version:      "698-i18n-seed-product-stock-unknown",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key = 'admin.products.stock.unknown'",
		SQL:          mustSQL("698_product_stock_unknown_i18n.sql"),
	})
}
