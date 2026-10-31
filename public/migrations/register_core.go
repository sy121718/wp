package migrations

func registerCoreSchemaAndAccess() {
	register(Migration{
		Version:   "001-init-schema",
		TableName: "sys_admin",
		SQL:       mustSQL("init_schema.sql"),
	})
	register(Migration{
		Version:   "002-init-builder-schema",
		TableName: "projects",
		SQL:       mustSQL("init_builder_schema.sql"),
	})
	register(Migration{
		Version:   "010-page-revisions",
		TableName: "page_revisions",
		SQL:       mustSQL("010_page_revisions.sql"),
	})
	register(Migration{
		Version:   "020-themes",
		TableName: "themes",
		SQL:       mustSQL("020_themes.sql"),
	})
	register(Migration{
		Version:   "021-blocks",
		TableName: "blocks",
		SQL:       mustSQL("021_blocks.sql"),
	})

	registerSeed(Seed{
		Version:   "030-business-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN " +
			"('page:list','project:list','block:list','media:list','artifact:detail','publication:receipts_pending')",
		SQL: mustSQL("030_business_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "030a-default-admin",
		TableName:    "sys_admin",
		ConditionSQL: "SELECT COUNT(*) FROM sys_admin WHERE username = 'admin'",
		SQL:          mustSQL("030a_default_admin.sql"),
	})
	registerSeed(Seed{
		Version:      "031-business-permissions-superadmin",
		TableName:    "sys_casbin_rule",
		ConditionSQL: "SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v1 = '/api/page/list' AND v0 IN (SELECT CAST(id AS VARCHAR) FROM sys_admin WHERE is_admin = 1)",
		SQL:          mustSQL("031_business_permissions_superadmin.sql"),
	})

	register(Migration{
		Version:   "040-plugin-registry",
		TableName: "plugin_registry",
		SQL:       mustSQL("040_plugin_registry.sql"),
	})

	register(Migration{
		Version:   "042-content",
		TableName: "contents",
		SQL:       mustSQL("042_content.sql"),
	})

	register(Migration{
		Version:   "046-navigation",
		TableName: "navigations",
		SQL:       mustSQL("046_navigation.sql"),
	})

	register(Migration{
		Version:   "047-block-category",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'category'",
		SQL:       mustSQL("047_block_category.sql"),
	})

	register(Migration{
		Version:   "049-block-reuse-mode",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'reuse_mode'",
		SQL:       mustSQL("049_block_reuse_mode.sql"),
	})

	register(Migration{
		Version:   "054-navigation-sources",
		TableName: "navigations",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'source_type'",
		SQL:       mustSQL("054_navigation_sources.sql"),
	})

	registerSeed(Seed{
		Version:      "032-plugin-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module = 'plugin'",
		SQL:          mustSQL("032_plugin_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "033-content-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module = 'content'",
		SQL:          mustSQL("033_content_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "034-content-pipeline-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('contenttemplate','presentation')",
		SQL:          mustSQL("034_content_pipeline_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "036-blueprint-navigation-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('blueprint','navigation')",
		SQL:          mustSQL("036_blueprint_navigation_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "035-theme-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'project:theme_%'",
		SQL:          mustSQL("035_theme_permissions.sql"),
	})

	register(Migration{
		Version:   "037-theme-active-unique",
		TableName: "themes",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'uq_themes_project_active'",
		SQL:       mustSQL("037_theme_active_unique.sql"),
	})

	register(Migration{
		Version:   "038-blocks-name-lower-unique",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'uq_blocks_project_name_lower'",
		SQL:       mustSQL("038_blocks_name_lower_unique.sql"),
	})

	register(Migration{
		Version:   "048-media-variant",
		TableName: "sys_media_variant",
		SQL:       mustSQL("048_media_variant.sql"),
	})
	registerSeed(Seed{
		Version:      "048-media-variant-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('media:download','media:download_batch','media:variants_generate')",
		SQL:          mustSQL("048_media_variant.sql"),
	})

	registerSeed(Seed{
		Version:      "050-admin-domains-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('admin','role','permission','menu','dept','datarule')",
		SQL:          mustSQL("050_admin_domains_permissions.sql"),
	})

	register(Migration{
		Version:   "053-drop-route-page-fk",
		TableName: "page_routes",

		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM pg_constraint WHERE conname = 'page_routes_page_id_fkey' AND conrelid = ?::regclass",
		SQL:      mustSQL("053_drop_route_page_fk.sql"),
	})

	registerSeed(Seed{
		Version:      "052-menu-icons",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND (icon IS NULL OR icon = '' OR icon LIKE 'i-ep:%')",
		SQL:          mustSQL("052_menu_icons.sql"),
	})

	registerSeed(Seed{
		Version:   "224-admin-nav-menu-rebuild",
		TableName: "sys_menus",

		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 7 THEN 1 ELSE 0 END FROM sys_menus " +
			"WHERE deleted_at IS NULL AND (" +
			"(type = 1 AND title IN ('管理', '内容', '商品与库存', '订单', '站点', '系统')) " +
			"OR (type = 2 AND path = '/admin'))",
		SQL: mustSQL("224_admin_nav_menu_rebuild.sql"),
	})

	register(Migration{
		Version:   "225-remove-theme-bundle-and-site-slots-menu",
		TableName: "sys_permission",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM (" +
			"SELECT permission_code FROM sys_permission WHERE (CAST(? AS text) IS NOT NULL) " +
			"AND permission_code IN ('project:theme_export', 'project:theme_import') " +
			"UNION ALL SELECT title FROM sys_menus WHERE type = 2 AND permission_code = 'page:site_slot_list' " +
			"AND path IN ('/site-slots', '/admin/site-slots') " +
			"UNION ALL SELECT v3 FROM sys_casbin_rule WHERE ptype = 'p' " +
			"AND v3 IN ('project:theme_export', 'project:theme_import')" +
			") AS leftover",
		SQL: mustSQL("225_remove_theme_bundle_and_site_slots_menu.sql"),
	})

	registerSeed(Seed{
		Version:   "226-i18n-seed-datarule-errors",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('ErrRuleConfigInvalid', 'ErrRuleFieldNotAllowed', " +
			"'ErrRuleLogicNotAllowed', 'ErrRuleOpNotAllowed')",
		SQL: mustSQL("226_i18n_seed_datarule_errors.sql"),
	})

	registerSeed(Seed{
		Version:   "227-i18n-seed-common-actions",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('admin.common.action.cancel', 'admin.common.action.save', " +
			"'admin.common.action.edit', 'admin.common.action.create')",
		SQL: mustSQL("227_i18n_seed_common_actions.sql"),
	})

	registerSeed(Seed{
		Version:   "228b-i18n-seed-role-permissions",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 15 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('MsgRolePermissionsTitle', " +
			"'admin.roles.action.permissions', 'admin.roles.perm.title', 'admin.roles.perm.intro', " +
			"'admin.roles.perm.back', 'admin.roles.perm.ph.search', 'admin.roles.perm.action.all', " +
			"'admin.roles.perm.action.none', 'admin.roles.perm.action.expand', " +
			"'admin.roles.perm.action.collapse', 'admin.roles.perm.hint.search', 'admin.roles.perm.empty', " +
			"'admin.roles.perm.toggle', 'admin.roles.perm.badge.disabled', 'admin.roles.perm.action.save')",
		SQL: mustSQL("228_i18n_seed_role_permissions.sql"),
	})

	registerSeed(Seed{
		Version:   "999-superadmin-all-policies",
		TableName: "sys_casbin_rule",
		ConditionSQL: `SELECT CASE WHEN EXISTS (
			SELECT 1 FROM sys_admin a CROSS JOIN sys_permission p
			WHERE a.is_admin = 1 AND p.status = 1 AND p.api_path <> ''
			  AND NOT EXISTS (
			      SELECT 1 FROM sys_casbin_rule r
			      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
			        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
			  )
		) THEN 0 ELSE 1 END`,
		SQL: mustSQL("051_superadmin_all_policies.sql"),
	})

	register(Migration{
		Version:   "267-publication-receipts-pending-index",
		TableName: "publication_receipts",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'idx_publication_receipts_pending'",
		SQL:       mustSQL("267_publication_receipts_pending_index.sql"),
	})

	register(Migration{
		Version:   "281-presentation-override-document",
		TableName: "presentation_instances",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'override_document'",
		SQL:       mustSQL("281_presentation_override_document.sql"),
	})

	register(Migration{
		Version:   "282-presentation-render-mode",
		TableName: "presentation_instances",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'render_mode'",
		SQL:       mustSQL("282_presentation_render_mode.sql"),
	})

	register(Migration{
		Version:   "285-navigation-mobile-panel",
		TableName: "navigations",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'panel_block_id'",
		SQL:       mustSQL("285_navigation_mobile_panel.sql"),
	})

	register(Migration{
		Version:   "289-dependency-kind-navigation",
		TableName: "page_dependencies",
		CheckSQL: "SELECT COUNT(*) FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid" +
			" JOIN pg_namespace n ON n.oid = t.relnamespace WHERE n.nspname = current_schema()" +
			" AND t.relname = ?" +
			" AND c.conname = 'page_dependencies_dependency_kind_check'" +
			" AND pg_get_constraintdef(c.oid) LIKE '%navigation%'",
		SQL: mustSQL("289_dependency_kind_navigation.sql"),
	})

	registerSeed(Seed{
		Version:      "269-navigation-err-internal",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'navigation.err.internal'",
		SQL:          mustSQL("269_navigation_err_internal_i18n.sql"),
	})

	registerSeed(Seed{
		Version:      "286-presentation-mode-errors-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN ('presentation.err.detachConfirmRequired','presentation.err.rollbackTargetMiss','presentation.err.rollbackFailed','presentation.err.snapshotMismatch')",
		SQL:          mustSQL("286_presentation_mode_errors_i18n.sql"),
	})

	registerSeed(Seed{
		Version:      "288-contenttemplate-activate-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code = 'contenttemplate:activate'",
		SQL:          mustSQL("288_contenttemplate_activate_permission.sql"),
	})

	registerSeed(Seed{
		Version:      "277-shell-bulk-ids-too-many-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'shell.err.bulkIdsTooMany'",
		SQL:          mustSQL("277_i18n_seed_shell_bulk_ids.sql"),
	})

	registerSeed(Seed{
		Version:      "280-inventory-action-done-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.inventory.actionDone'",
		SQL:          mustSQL("280_inventory_action_done_i18n.sql"),
	})

	register(Migration{
		Version:   "401-hide-mail-campaign-menu",
		TableName: "sys_menus",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM (" +
			"SELECT id FROM sys_menus WHERE (CAST(? AS text) IS NOT NULL) " +
			"AND type = 2 AND path = '/admin/mail/campaign' AND is_hidden = 0 AND deleted_at IS NULL" +
			") AS leftover",
		SQL: mustSQL("401_hide_mail_campaign_menu.sql"),
	})
}
