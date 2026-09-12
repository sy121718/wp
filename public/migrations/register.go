// 本文件注册全部数据库迁移：按 Version 字符串排序执行。
//
// 各迁移通过代表性表名做幂等存在性检查；SQL 文本由 SplitStatements
// 按语句边界（含 DO $$ 块）安全拆分后逐条执行。
package migrations

import _ "embed"

//go:embed init_schema.sql
var initSchemaSQL string

//go:embed init_builder_schema.sql
var initBuilderSchemaSQL string

//go:embed 010_page_revisions.sql
var pageRevisionsSQL string

//go:embed 020_themes.sql
var themesSQL string

//go:embed 021_blocks.sql
var blocksSQL string

//go:embed 030_business_permissions.sql
var businessPermSQL string

//go:embed 031_business_permissions_superadmin.sql
var businessPermSuperAdminSQL string

//go:embed 032_plugin_permissions.sql
var pluginPermSQL string

//go:embed 040_plugin_registry.sql
var pluginRegistrySQL string

//go:embed 042_content.sql
var contentSQL string

//go:embed 033_content_permissions.sql
var contentPermSQL string

//go:embed 034_content_pipeline_permissions.sql
var contentPipelinePermSQL string

//go:embed 046_navigation.sql
var navigationSQL string

//go:embed 047_block_category.sql
var blockCategorySQL string

//go:embed 036_blueprint_navigation_permissions.sql
var blueprintNavigationPermSQL string

//go:embed 035_theme_permissions.sql
var themePermSQL string

//go:embed 037_theme_active_unique.sql
var themeActiveUniqueSQL string

//go:embed 038_blocks_name_lower_unique.sql
var blocksNameLowerUniqueSQL string

//go:embed 048_media_variant.sql
var mediaVariantSQL string

//go:embed 049_block_reuse_mode.sql
var blockReuseModeSQL string

//go:embed 050_admin_domains_permissions.sql
var adminDomainsPermSQL string

//go:embed 051_superadmin_all_policies.sql
var superadminAllPoliciesSQL string

//go:embed 052_menu_icons.sql
var menuIconsSQL string

//go:embed 053_drop_route_page_fk.sql
var dropRoutePageFKSQL string

//go:embed 054_navigation_sources.sql
var navigationSourcesSQL string

//go:embed 055_i18n_columns.sql
var i18nColumnsSQL string

//go:embed 056_i18n_revision.sql
var i18nRevisionSQL string

//go:embed 057_sys_menus_title_key.sql
var menuTitleKeySQL string

//go:embed 058_i18n_seed_enums.sql
var i18nSeedEnumsSQL string

//go:embed 059_i18n_seed_shell.sql
var i18nSeedShellSQL string

//go:embed 060_i18n_seed_site_components.sql
var i18nSeedSiteComponentsSQL string

//go:embed 061_page_artifacts_lang.sql
var pageArtifactsLangSQL string

//go:embed 062_page_publications.sql
var pagePublicationsSQL string

//go:embed 063_page_stagings.sql
var pageStagingsSQL string

//go:embed 064_project_locales.sql
var projectLocalesSQL string

//go:embed 065_i18n_seed_language_switcher.sql
var i18nSeedLanguageSwitcherSQL string

//go:embed 066_sys_translation.sql
var sysTranslationSQL string

//go:embed 067_media_center.sql
var mediaCenterSQL string

//go:embed 068_pg_jsonb_partial_index.sql
var pgJSONBPartialIndexSQL string

//go:embed 069_drop_obsolete_design_tables.sql
var dropObsoleteDesignTablesSQL string

//go:embed 070_sys_status_index_audit.sql
var sysStatusIndexAuditSQL string

//go:embed 071_dependency_fanout.sql
var dependencyFanoutSQL string

//go:embed 072_media_replace_permissions.sql
var mediaReplacePermsSQL string

//go:embed 074_attachment_md5_unique.sql
var attachmentMD5UniqueSQL string

//go:embed 075_navigation_path_unique.sql
var navigationPathUniqueSQL string

//go:embed 076_lang_backfill_per_project.sql
var langBackfillPerProjectSQL string

//go:embed 077_recovery_permissions.sql
var recoveryPermsSQL string

//go:embed 078_artifact_gc_permissions.sql
var artifactGCPermsSQL string

//go:embed 079_content_collections_permission.sql
var contentCollectionsPermSQL string

//go:embed 080_content_type_narrowing.sql
var contentTypeNarrowingSQL string

//go:embed 081_product_tables.sql
var productTablesSQL string

//go:embed 082_product_permissions.sql
var productPermsSQL string

//go:embed 083_product_variant_options_unique.sql
var productVariantOptionsUniqueSQL string

//go:embed 084_product_menu.sql
var productMenuSQL string

//go:embed 085_product_detail_template.sql
var productDetailTemplateSQL string

//go:embed 086_product_attribute_specs.sql
var productAttributeSpecsSQL string

//go:embed 086a_product_attribute_permissions.sql
var productAttributePermsSQL string

//go:embed 086b_product_attribute_menu.sql
var productAttributeMenuSQL string

//go:embed 087_product_variant_generate_permissions.sql
var productVariantGeneratePermsSQL string

//go:embed 087b_product_variant_options_template.sql
var productVariantOptionsTemplateSQL string

//go:embed 088_product_taxonomy.sql
var productTaxonomySQL string

//go:embed 089_product_taxonomy_permissions.sql
var productTaxonomyPermsSQL string

//go:embed 090_product_taxonomy_menu.sql
var productTaxonomyMenuSQL string

//go:embed 091_product_tags.sql
var productTagsSQL string

//go:embed 092_product_tag_permissions.sql
var productTagPermsSQL string

//go:embed 093_product_tag_menu.sql
var productTagMenuSQL string

//go:embed 094_product_image_alts.sql
var productImageAltsSQL string

//go:embed 095_product_pricing.sql
var productPricingSQL string

//go:embed 096_product_pricing_permissions.sql
var productPricingPermsSQL string

//go:embed 097_product_pricing_menu.sql
var productPricingMenuSQL string

//go:embed 098_product_detail_template_choice_permissions.sql
var productDetailTemplatePermsSQL string

//go:embed 099_inventory_tables.sql
var inventoryTablesSQL string

//go:embed 100_inventory_permissions.sql
var inventoryPermsSQL string

//go:embed 101_inventory_menu.sql
var inventoryMenuSQL string

//go:embed 102_inventory_movements.sql
var inventoryMovementsSQL string

//go:embed 103_inventory_reasons_seed.sql
var inventoryReasonsSeedSQL string

//go:embed 104_inventory_change_permissions.sql
var inventoryChangePermsSQL string

//go:embed 105_inventory_sources.sql
var inventorySourcesSQL string

//go:embed 106_inventory_source_permissions.sql
var inventorySourcePermsSQL string

//go:embed 107_inventory_source_menu.sql
var inventorySourceMenuSQL string

//go:embed 108_inventory_purchase.sql
var inventoryPurchaseSQL string

//go:embed 109_inventory_purchase_permissions.sql
var inventoryPurchasePermsSQL string

//go:embed 110_inventory_purchase_menu.sql
var inventoryPurchaseMenuSQL string

//go:embed 111_master_data_changes.sql
var masterDataChangesSQL string

//go:embed 112_master_data_permissions.sql
var masterDataPermsSQL string

//go:embed 113_master_data_menu.sql
var masterDataMenuSQL string

//go:embed 114_product_bundle_config.sql
var productBundleConfigSQL string

//go:embed 115_product_bundle_permissions.sql
var productBundlePermsSQL string

//go:embed 116_product_bundle_menu.sql
var productBundleMenuSQL string

//go:embed 117_product_brand_index.sql
var productBrandIndexSQL string

//go:embed 118_product_variant_option_index.sql
var productVariantOptionIndexSQL string

//go:embed 119_product_variant_price_index.sql
var productVariantPriceIndexSQL string

//go:embed 120_product_rating.sql
var productRatingSQL string

//go:embed 121_drop_stock_cache.sql
var dropStockCacheSQL string

//go:embed 122_drop_cache_permissions.sql
var dropCachePermissionsSQL string

//go:embed 073_blueprint_ddl_align.sql
var blueprintDDLAlignSQL string

//go:embed 123_user.sql
var userSQL string

//go:embed 124_mail.sql
var mailSQL string

//go:embed 125_mail_marketing.sql
var mailMarketingSQL string

//go:embed 126_mail.sql
var mailPermissionSQL string

//go:embed 127_mail_log_links.sql
var mailLogLinksSQL string

//go:embed 128_mail_campaign.sql
var mailCampaignSQL string

//go:embed 129_mail_templates.sql
var mailBuiltinTemplatesSQL string

//go:embed 130_mail_template_variables.sql
var mailTemplateVarsSQL string

//go:embed 131_mail_automation.sql
var mailAutomationSQL string

//go:embed 132_mail_automation.sql
var mailAutomationPermSQL string

//go:embed 133_mail_automation_layout_perm.sql
var mailAutomationLayoutPermSQL string

//go:embed 134_inventory_reference_fks.sql
var inventoryReferenceFKsSQL string

//go:embed 135_order.sql
var orderTablesSQL string

//go:embed 136_order_permissions.sql
var orderPermsSQL string

func init() {
	register(Migration{
		Version:   "001-init-schema",
		TableName: "sys_admin",
		SQL:       initSchemaSQL,
	})
	register(Migration{
		Version:   "002-init-builder-schema",
		TableName: "projects",
		SQL:       initBuilderSchemaSQL,
	})
	register(Migration{
		Version:   "010-page-revisions",
		TableName: "page_revisions",
		SQL:       pageRevisionsSQL,
	})
	register(Migration{
		Version:   "020-themes",
		TableName: "themes",
		SQL:       themesSQL,
	})
	register(Migration{
		Version:   "021-blocks",
		TableName: "blocks",
		SQL:       blocksSQL,
	})

	// 业务权限 seed（权限点 + 菜单 + 超管全量策略）。
	// 执行入口：internal/routers/routes.go 路由装配时调用 RunSeeds（幂等）。
	registerSeed(Seed{
		Version:      "030-business-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('page','project','block','media','artifact','publication')",
		SQL:          businessPermSQL,
	})
	registerSeed(Seed{
		Version:      "031-business-permissions-superadmin",
		TableName:    "sys_casbin_rule",
		ConditionSQL: "SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v1 = '/api/page/list' AND v0 IN (SELECT CAST(id AS VARCHAR) FROM sys_admin WHERE is_admin = 1)",
		SQL:          businessPermSuperAdminSQL,
	})

	// 插件注册表（docs/06-plugin-system.md §8：安装/版本/启停记账）。
	register(Migration{
		Version:   "040-plugin-registry",
		TableName: "plugin_registry",
		SQL:       pluginRegistrySQL,
	})

	// CMS 内容实体（docs/02-domain.md §1，0-A2 content 模块）。
	register(Migration{
		Version:   "042-content",
		TableName: "contents",
		SQL:       contentSQL,
	})

	// 内容结构模板（docs/02-domain.md §2，0-A2 contenttemplate 模块）与自动发布实例
	// （§3，presentation 模块）：建表由 002-init-builder-schema 承担。
	//
	// 原 043_content_template.sql / 044_presentation.sql 是同一批表的重复定义，
	// 且列集合明显更旧（缺 project_id NOT NULL、缺 current_version_id），
	// 因 002 先建表而永远被跳过 —— 与 045_blueprint.sql 同因，已一并删除。
	// DDL 唯一真源为 002；model 已对齐 002 的实际列集合（见 28031ed）。

	// Page 初始化工具 Blueprint：建表由 002-init-builder-schema 承担。
	// 原 045_blueprint.sql 是同一张表的重复定义（列集合还与 model 矛盾），
	// 因 002 先建表而永远被跳过 —— 已删除，DDL 唯一真源为 002。
	// 历史库的结构对齐见迁移 073-blueprint-ddl-align。

	// 公开站点导航（docs/05 阶段6，0-C）。
	register(Migration{
		Version:   "046-navigation",
		TableName: "navigations",
		SQL:       navigationSQL,
	})

	// 全局块自由分类（组织/筛选维度，不改引用维度）。
	// blocks 表已在 021 创建，默认幂等检查（表存在即跳过）会误跳过，
	// 故用自定义 CheckSQL 按 category 列是否存在判断。
	register(Migration{
		Version:   "047-block-category",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'category'",
		SQL:       blockCategorySQL,
	})

	// 全局块复用方式维度（docs/02-D：global 引用 / template 一次性复制）。
	// blocks 表已在 021 创建，默认幂等检查会误跳过，仿 047 按 reuse_mode 列是否存在判断。
	register(Migration{
		Version:   "049-block-reuse-mode",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'reuse_mode'",
		SQL:       blockReuseModeSQL,
	})

	// 导航项多来源（对齐 WP 菜单：页面/文章/产品/分类/全局块 + 打开方式）。
	register(Migration{
		Version:   "054-navigation-sources",
		TableName: "navigations",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'source_type'",
		SQL:       navigationSourcesSQL,
	})

	// 插件权限 seed（权限点 + 后台菜单，docs/06）。
	registerSeed(Seed{
		Version:      "032-plugin-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module = 'plugin'",
		SQL:          pluginPermSQL,
	})

	// CMS 内容权限 seed（权限点 + 超管策略，0-A2）。
	registerSeed(Seed{
		Version:      "033-content-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module = 'content'",
		SQL:          contentPermSQL,
	})

	// 内容模板与自动发布权限 seed（0-A2）。
	registerSeed(Seed{
		Version:      "034-content-pipeline-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('contenttemplate','presentation')",
		SQL:          contentPipelinePermSQL,
	})

	// Blueprint 与 Navigation 权限 seed（0-B/0-C）。
	registerSeed(Seed{
		Version:      "036-blueprint-navigation-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('blueprint','navigation')",
		SQL:          blueprintNavigationPermSQL,
	})

	// 主题（Theme）权限 seed（project 模块下 theme 能力，此前无权限点体系）。
	registerSeed(Seed{
		Version:      "035-theme-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'project:theme_%'",
		SQL:          themePermSQL,
	})

	// 主题「同工程单激活」部分唯一索引（themes 表已由 020 创建，默认表存在检查会误跳过，
	// 故用自定义 CheckSQL 按索引名判断是否存在）。
	register(Migration{
		Version:   "037-theme-active-unique",
		TableName: "themes",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'uq_themes_project_active'",
		SQL:       themeActiveUniqueSQL,
	})

	// blocks 表「同工程块名大小写不敏感」唯一索引（blocks 已由 021 创建，默认表存在检查会误跳过，
	// 故用自定义 CheckSQL 按索引名判断是否存在）。
	register(Migration{
		Version:   "038-blocks-name-lower-unique",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'uq_blocks_project_name_lower'",
		SQL:       blocksNameLowerUniqueSQL,
	})

	// 媒体图片变体表（thumb/medium/webp），同文件内附带 media 下载/变体接口权限点 seed。
	// Migration 负责建表；Seed 由 RunSeeds 兜底保证权限点存在
	// （DDL IF NOT EXISTS / INSERT NOT EXISTS 均幂等，重复执行安全）。
	register(Migration{
		Version:   "048-media-variant",
		TableName: "sys_media_variant",
		SQL:       mediaVariantSQL,
	})
	registerSeed(Seed{
		Version:      "048-media-variant-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('media:download','media:download_batch','media:variants_generate')",
		SQL:          mediaVariantSQL,
	})

	// 管理面六领域权限 seed（权限点 + 超管策略）：六领域 API 挂 Casbin 但此前从未 seed，
	// 超管访问 /api/role/* 等也被拒。幂等，重启或 RunSeeds 时生效。
	registerSeed(Seed{
		Version:      "050-admin-domains-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('admin','role','permission','menu','dept','datarule')",
		SQL:          adminDomainsPermSQL,
	})

	// 超管全量策略补全：从 sys_permission 全表 CROSS JOIN 生成。
	// 修正「各 seed 硬编码策略清单 + ConditionSQL 跳过导致策略缺失」的历史问题
	//（实测超管缺 plugin/theme/content 等策略，后台对应操作 403）。
	// 后台菜单/目录/按钮图标补全（历史数据无图标或旧格式 i-ep:*）。
	// 去掉 page_routes.page_id 外键：预留路径时页面尚未创建，外键与预留语义冲突
	//（导致「新建页面」必然 500）。
	register(Migration{
		Version:   "053-drop-route-page-fk",
		TableName: "page_routes",
		CheckSQL:  "SELECT COUNT(*) FROM pg_constraint WHERE conname = 'page_routes_page_id_fkey' AND conrelid = ?::regclass",
		SQL:       dropRoutePageFKSQL,
	})

	registerSeed(Seed{
		Version:      "052-menu-icons",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE deleted_time IS NULL AND (icon IS NULL OR icon = '' OR icon LIKE 'i-ep:%')",
		SQL:          menuIconsSQL,
	})

	// 超管全量策略补全（每次启动检查，缺哪条补哪条）。
	//
	// ConditionSQL 语义是「返回 > 0 则跳过整个 seed」，因此这里必须表达
	// 「缺失条数为 0」才跳过：原实现统计的是「超管已有多少条策略」，
	// 首次执行后恒 > 0 → 之后新增的权限点永远不会补超管策略，
	// 表现为新接口对超管也 403（media:replace / media:references 即此因）。
	//
	// Version 用 999 前缀排在全部 seed 之后：本条做的是「超管 = 全部启用权限点」的
	// 全量补全，必须在所有权限点 seed 执行完之后才检查。用 051 前缀时它先于 072 等
	// 新权限点 seed 运行，检查时「没有缺失」→ 直接跳过，之后新插入的权限点永远补不上。
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
		SQL: superadminAllPoliciesSQL,
	})

	// 072：补 media 换图 / 引用来源权限点（此前从未 seed，接口对全员 403 死链）。
	registerSeed(Seed{
		Version:      "072-media-replace-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('media:replace','media:references')",
		SQL:          mediaReplacePermsSQL,
	})

	// 074 / 075：把两处「先查后写」的判断（媒体上传去重、导航路径唯一）落到
	// 数据库层唯一约束 —— 并发下两次请求都能通过检查，只有约束能真正兜住。
	// CheckSQL 用 pg_indexes 判断索引是否已存在（默认的「表是否存在」对加索引无意义）。
	register(Migration{
		Version:   "074-attachment-md5-unique",
		TableName: "uq_attachment_md5_type_active",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE indexname = ?",
		SQL:       attachmentMD5UniqueSQL,
	})
	register(Migration{
		Version:   "075-navigation-path-unique",
		TableName: "uq_navigation_project_kind_path",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE indexname = ?",
		SQL:       navigationPathUniqueSQL,
	})

	// 076：按工程修正 061 回填的语言（原实现取全局第一个工程的 defaultLang，多工程库全被标成同一语言）。
	// TableName 故意用不存在的名字：默认 CheckSQL（表是否存在）恒为 0，等价于「每次启动都跑一遍」。
	// 该 UPDATE 幂等且带 IS DISTINCT FROM 条件，无差异时零行更新。
	register(Migration{
		Version:   "076-lang-backfill-per-project",
		TableName: "page_artifacts_lang_backfill_always",
		SQL:       langBackfillPerProjectSQL,
	})

	// 077：灾难恢复接口权限点（产物重建 + 激活面巡检）。
	registerSeed(Seed{
		Version:      "077-recovery-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('page:artifact_rebuild','page:publication_audit')",
		SQL:          recoveryPermsSQL,
	})

	// 078：产物回收接口权限点。
	registerSeed(Seed{
		Version:      "078-artifact-gc-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'page:artifact_gc'",
		SQL:          artifactGCPermsSQL,
	})

	// 079：内容集合元数据接口权限点（工作台集合字段下拉 + 内置组件字段校验）。
	registerSeed(Seed{
		Version:      "079-content-collections-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'content:collections'",
		SQL:          contentCollectionsPermSQL,
	})

	// 087：变体组合生成权限点 + 超管策略（issue #8）。条件只看本票自己的权限点，
	// 与 082/086a 的宽匹配（product:% / product:attribute_%）互不干扰。
	registerSeed(Seed{
		Version:      "087-product-variant-generate-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'product:variant_generate'",
		SQL:          productVariantGeneratePermsSQL,
	})

	// 087b：默认商品详情模板补规格槽位（issue #8）。085 只在首次建库执行，
	// 已执行过的库不会重跑，故这里补一次；DO 块内只重写与 085 原样一致的模板，
	// 作者改过的模板不动（缺槽位是作者的选择）。
	//
	// ConditionSQL 的语义是「已存在则跳过」（与其他种子一致）：所有 product 模板
	// 都已带 optionsField 时返回 1 跳过；仍有缺槽位的模板时返回 0 → 跑 DO 块。
	registerSeed(Seed{
		Version:      "087b-product-variant-options-template",
		TableName:    "content_templates",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM content_templates WHERE entity_type = 'product' AND draft_document::text NOT LIKE '%\"optionsField\"%'",
		SQL:          productVariantOptionsTemplateSQL,
	})

	// 088：商品 → 主分类列（issue #10）。product_categories / product_brands 两张表
	// 由 081 建好，本迁移只补 products.primary_category_id 与其索引。
	// products 早已存在，默认「表存在即跳过」必然误跳过，故按列是否存在判定
	// （与 067/086 同一手法）。
	register(Migration{
		Version:   "088-product-taxonomy",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'primary_category_id'",
		SQL: productTaxonomySQL,
	})

	// 089：分类与品牌权限点 + 超管策略（issue #10）。
	// 条件只看本票自己的权限点，与 082 的宽匹配（product:%）互不干扰。
	registerSeed(Seed{
		Version:   "089-product-taxonomy-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:category_list', 'product:category_get', 'product:category_create', " +
			"'product:category_update', 'product:category_delete', " +
			"'product:brand_list', 'product:brand_get', 'product:brand_create', " +
			"'product:brand_update', 'product:brand_delete')",
		SQL: productTaxonomyPermsSQL,
	})

	// 090：分类与品牌后台菜单（issue #10）。须在 084（商品管理菜单）之后执行，
	// 否则父菜单还不存在，COALESCE 会把两个入口落到顶级。
	registerSeed(Seed{
		Version:      "090-product-taxonomy-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_menus WHERE title IN ('商品分类', '商品品牌') AND type = 2 AND deleted_time IS NULL",
		SQL:          productTaxonomyMenuSQL,
	})

	// 091：商品标签规则化（issue #11）—— product_tags 由 081 建好、products 早已存在，
	// 本迁移补 published_at（新品规则的时间基准）/ recalc_at（重算时间）+ 规则形状约束。
	// 两张表都早已存在，默认「表存在即跳过」必然误跳过，故按列是否存在判定：
	// 恰好一个 ? 参数（products），另一张表用字面量表名（与 070 同一手法）。
	register(Migration{
		Version:   "091-product-tags",
		TableName: "products",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name IN (?, 'product_tags') " +
			"AND column_name IN ('published_at', 'recalc_at')",
		SQL: productTagsSQL,
	})

	// 092：标签权限点 + 超管策略（issue #11）。
	// 条件只看本票自己的权限点，与 082 的宽匹配（product:%）互不干扰。
	registerSeed(Seed{
		Version:   "092-product-tag-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:tag_list', 'product:tag_get', 'product:tag_products', 'product:tag_rule_types', " +
			"'product:tag_create', 'product:tag_update', 'product:tag_delete', 'product:tag_recalc')",
		SQL: productTagPermsSQL,
	})

	// 093：标签后台菜单（issue #11）。须在 084（商品管理菜单）之后执行，
	// 否则父菜单还不存在，COALESCE 会把入口落到顶级。
	registerSeed(Seed{
		Version:      "093-product-tag-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '商品标签' AND type = 2 AND deleted_time IS NULL",
		SQL:          productTagMenuSQL,
	})

	// 095：商品定价工具留痕（issue #13）—— 两张全新表（调价批次 + 逐变体明细）。
	// 表此前不存在，默认「表存在即跳过」即可；定价结果写回 product_variants.price，
	// 不新增任何构建期读取路径（不进构建管线）。
	register(Migration{
		Version:   "095-product-pricing",
		TableName: "product_price_adjustments",
		SQL:       productPricingSQL,
	})

	// 096：定价工具权限点 + 超管策略（issue #13）。
	// 条件只看本票自己的权限点，与 082 的宽匹配（product:%）互不干扰。
	registerSeed(Seed{
		Version:   "096-product-pricing-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:pricing_rules', 'product:pricing_roundings', 'product:pricing_preview', " +
			"'product:pricing_apply', 'product:pricing_history', 'product:pricing_adjustment')",
		SQL: productPricingPermsSQL,
	})

	// 097：定价工具后台菜单（issue #13）。须在 084（商品管理菜单）之后执行，
	// 否则父菜单还不存在，COALESCE 会把入口落到顶级。
	registerSeed(Seed{
		Version:      "097-product-pricing-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '定价工具' AND type = 2 AND deleted_time IS NULL",
		SQL:          productPricingMenuSQL,
	})

	// 098：详情页模板可选与预览权限点 + 超管策略（issue #14）。
	// 两个新接口（preview 只读渲染 / get-by-entity 读当前绑定）各自一个权限点；
	// 后台「详情页模板」页的写动作复用既有 presentation:create / presentation:rebuild。
	registerSeed(Seed{
		Version:   "098-product-detail-template-choose-perms",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'presentation:preview', 'presentation:get_by_entity')",
		SQL: productDetailTemplatePermsSQL,
	})

	// 094：商品图集 alt 文本列（issue #12 商品多语言）。
	// products 由 081 创建，默认「表存在即跳过」必然误跳过，故按列是否存在判定
	// （与 067/091 同一手法）：images_alt 列存在即视为已完成。
	register(Migration{
		Version:   "094-product-image-alts",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'images_alt'",
		SQL: productImageAltsSQL,
	})

	// 086：商品属性组与属性值（issue #7）—— product_attributes 由 081 建好，
	// 本迁移只在其上补 key 唯一的部分索引 + 回填历史空 key + is_variation 显式 CHECK。
	// 表早已存在，默认「表存在即跳过」必然误跳过，故按索引名判定（与 037/038/068 同一手法）。
	register(Migration{
		Version:   "086-product-attribute-specs",
		TableName: "product_attributes",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'attribute_ids'",
		SQL: productAttributeSpecsSQL,
	})

	// 086a：属性组权限点 + 超管策略（issue #7）。条件与 082 互斥（product:attribute_%）。
	registerSeed(Seed{
		Version:      "086a-product-attribute-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'product:attribute_%'",
		SQL:          productAttributePermsSQL,
	})

	// 086b：属性后台菜单（issue #7）。须在 084（商品管理菜单）之后执行，
	// 否则父菜单还不存在，COALESCE 会把「商品属性」落到顶级。
	registerSeed(Seed{
		Version:      "086b-product-attribute-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE title = '商品属性' AND type = 2",
		SQL:          productAttributeMenuSQL,
	})

	// 085：默认商品详情内容模板（issue #6）—— 商品发布按实体类型解析模板，
	// 种一份类型级默认模板即可让「新建商品即用上」，无需逐商品手工拼装。
	// 数据种子：内容模板行 + 首个不可变版本 + current_version_id 指针，
	// 由 DO 块一次写入（applySeed 以单条语句执行整段 SQL）。
	registerSeed(Seed{
		Version:      "085-product-detail-template",
		TableName:    "content_templates",
		ConditionSQL: "SELECT COUNT(*) FROM content_templates WHERE entity_type = 'product'",
		SQL:          productDetailTemplateSQL,
	})

	// 084：商品管理后台菜单（issue #5）。
	registerSeed(Seed{
		Version:      "084-product-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE title = '商品管理' AND type = 2",
		SQL:          productMenuSQL,
	})

	// 083：修正 product_variants 的规格组合唯一约束（改部分唯一索引）。
	// 按「索引已存在且旧约束已消失」判定跳过。
	register(Migration{
		Version:   "083-product-variant-options-unique",
		TableName: "product_variants",
		CheckSQL: `SELECT COUNT(*) FROM pg_indexes i
			WHERE i.schemaname = current_schema() AND i.tablename = ?
			  AND i.indexname = 'uq_product_variants_product_options'
			  AND NOT EXISTS (
			      SELECT 1 FROM pg_constraint c
			      WHERE c.conrelid = 'product_variants'::regclass
			        AND c.conname = 'product_variants_product_id_option_values_key'
			  )`,
		SQL: productVariantOptionsUniqueSQL,
	})

	// 082：商品域权限点 + 超管策略（issue #5）。
	registerSeed(Seed{
		Version:      "082-product-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'product:%'",
		SQL:          productPermsSQL,
	})

	// 081：商品域六张表（issue #5）。products 是新建表，默认「表存在即跳过」即可。
	register(Migration{
		Version:   "081-product-tables",
		TableName: "products",
		SQL:       productTablesSQL,
	})

	// 080：内容类型收敛 —— contents 只保留 article；content_templates 解掉类型枚举
	// （合法性交给实体类型注册表）；pages 内容契约去掉 product / category 分支。
	// 三张表都由更早的迁移建立，本迁移只改约束，故按「约束已收敛」判定跳过。
	register(Migration{
		Version:   "080-content-type-narrowing",
		TableName: "contents",
		CheckSQL: `SELECT COUNT(*) FROM pg_constraint
			WHERE conrelid = ?::regclass
			  AND conname = 'contents_entity_type_check'
			  AND pg_get_constraintdef(oid) LIKE '%article%'
			  AND pg_get_constraintdef(oid) NOT LIKE '%product%'`,
		SQL: contentTypeNarrowingSQL,
	})

	// 099：仓库与库存记录（issue #15）。两张新表（inventory_warehouses / inventory_stocks），
	// 默认「表存在即跳过」检查即可 —— 库存真源与仓库实体都是本票新建的对象。
	register(Migration{
		Version:   "099-inventory-tables",
		TableName: "inventory_warehouses",
		SQL:       inventoryTablesSQL,
	})

	// 100：仓库与库存权限点 + 超管策略（issue #15）。
	// 条件只看本票自己的权限点（inventory:%），与其它模块的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "100-inventory-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 9 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:warehouse_list', 'inventory:warehouse_get', 'inventory:warehouse_create', " +
			"'inventory:warehouse_update', 'inventory:warehouse_delete', " +
			"'inventory:stock_list', 'inventory:stock_sku', 'inventory:stock_get', 'inventory:stock_ensure')",
		SQL: inventoryPermsSQL,
	})

	// 101：库存管理后台菜单（issue #15）。须在 084（商品管理菜单）之后执行，
	// 否则「站点工程」目录还不存在时 COALESCE 会把入口落到顶级。
	registerSeed(Seed{
		Version:      "101-inventory-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '库存管理' AND type = 2 AND deleted_time IS NULL",
		SQL:          inventoryMenuSQL,
	})

	// 102：库存流水 / 变动原因字典 / 物料清单 / 缓存同步台账（issue #16）。
	// 四张全新表，默认「表存在即跳过」检查即可；库存**真源** inventory_stocks
	// 的结构一个字不动（行锁加在既有表既有的行上）。
	register(Migration{
		Version:   "102-inventory-movements",
		TableName: "inventory_change_reasons",
		SQL:       inventoryMovementsSQL,
	})

	// 103：内置变动原因字典（in / out / adjust 三类各若干条，issue #16）。
	// 单条 INSERT：project_id IS NULL 表示内置（全工程可见）；
	// 幂等条件 = 每个内置 code 都已存在，新增内置原因后重启即补齐。
	registerSeed(Seed{
		Version:   "103-inventory-reasons-seed",
		TableName: "inventory_change_reasons",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 9 THEN 1 ELSE 0 END FROM inventory_change_reasons " +
			"WHERE project_id IS NULL AND lower(code) IN (" +
			"'purchase_in', 'return_in', 'transfer_in', 'production_in', " +
			"'sale_out', 'damage_out', 'transfer_out', 'stocktake_adjust', 'manual_adjust')",
		SQL: inventoryReasonsSeedSQL,
	})

	// 104：库存变动 / 流水 / 原因字典 / 物料清单 / 缓存对账 10 个权限点 + 超管策略（issue #16）。
	// 条件只看本票自己的权限点，与 100 的宽匹配（inventory:%）互不干扰。
	registerSeed(Seed{
		Version:   "104-inventory-change-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:stock_change', 'inventory:stock_deduct', 'inventory:movement_list', " +
			"'inventory:reason_list', 'inventory:reason_create', 'inventory:reason_update', " +
			"'inventory:bom_set', 'inventory:bom_get', 'inventory:cache_sync', 'inventory:cache_reconcile')",
		SQL: inventoryChangePermsSQL,
	})

	// 105：货源表（issue #17）。一张新表承载全部进货来源（外部供应商 / 集团内关联公司 /
	// 自家工厂），类型 + 关联方标志 + 异构对接配置（config）三件事各就各位。
	register(Migration{
		Version:   "105-inventory-sources",
		TableName: "inventory_sources",
		SQL:       inventorySourcesSQL,
	})

	// 106：货源管理 6 个权限点 + 超管策略（issue #17）。
	// 条件只看本票自己的权限点（inventory:source_%），与 100 的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "106-inventory-source-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:source_list', 'inventory:source_get', 'inventory:source_create', " +
			"'inventory:source_update', 'inventory:source_delete', 'inventory:source_summary')",
		SQL: inventorySourcePermsSQL,
	})

	// 107：货源管理后台菜单（issue #17）。须在 101（库存管理菜单）之后执行，
	// 且与它同挂「站点工程」目录（sort 9，排在库存管理 sort 8 之后）。
	registerSeed(Seed{
		Version:      "107-inventory-source-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '货源管理' AND type = 2 AND deleted_time IS NULL",
		SQL:          inventorySourceMenuSQL,
	})

	// 108：采购单与入库四张表（issue #18）：采购单头 / 采购行（含已入库数量）/
	// 入库单头（采购收货与自家工厂生产入库共用，带幂等键）/ 入库单行。
	// 四张全新表，默认「表存在即跳过」检查即可；库存**真源** inventory_stocks
	// 一张不加、一列不改 —— 入库一律经 #16 的变动契约写它。
	register(Migration{
		Version:   "108-inventory-purchase",
		TableName: "inventory_purchase_orders",
		SQL:       inventoryPurchaseSQL,
	})

	// 109：采购单与入库 7 个权限点 + 超管策略（issue #18）。
	// 条件只看本票自己的权限点（inventory:purchase_%），与 100 的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "109-inventory-purchase-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 7 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:purchase_list', 'inventory:purchase_get', 'inventory:purchase_create', " +
			"'inventory:purchase_update', 'inventory:purchase_receipt', " +
			"'inventory:purchase_production', 'inventory:purchase_history')",
		SQL: inventoryPurchasePermsSQL,
	})

	// 110：采购入库后台菜单（issue #18）。须在 101（库存管理菜单）之后执行，
	// 且与它同挂「站点工程」目录（sort 10，排在货源管理 sort 9 之后）。
	registerSeed(Seed{
		Version:      "110-inventory-purchase-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '采购入库' AND type = 2 AND deleted_time IS NULL",
		SQL:          inventoryPurchaseMenuSQL,
	})

	// 111：主数据变更记录表（issue #19）。一张 append-only 的字段级审计表，
	// 与库存流水职责分离（后者记数量变动，本表记字段级配置变更）。
	// 默认「表存在即跳过」在「建表成功但触发器没建成」时不安全 —— 那会让
	// append-only 静默失效，故 CheckSQL 同时核对表与触发器（都齐了才算完成；
	// 缺任意一个即重跑整段 SQL，语句全部幂等）。
	// 注意：migrator.apply 固定以 TableName 作为唯一 ? 参数调用 CheckSQL。
	register(Migration{
		Version:   "111-master-data-changes",
		TableName: "master_data_changes",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM (" +
			"SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"UNION ALL " +
			"SELECT 1 FROM pg_trigger t " +
			"JOIN pg_class c ON c.oid = t.tgrelid " +
			"JOIN pg_namespace n ON n.oid = c.relnamespace " +
			"WHERE n.nspname = current_schema() AND c.relname = 'master_data_changes' " +
			"AND t.tgname = 'trg_master_data_changes_append_only' AND NOT t.tgisinternal" +
			") x",
		SQL: masterDataChangesSQL,
	})

	// 112：主数据变更记录 4 个只读权限点 + 超管策略（issue #19）。
	// 条件只看本票自己的权限点（masterdata:change_%），与 100 的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "112-master-data-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'masterdata:change_list', 'masterdata:change_count', " +
			"'masterdata:change_entities', 'masterdata:change_entity')",
		SQL: masterDataPermsSQL,
	})

	// 113：变更记录后台菜单（issue #19）。须在 101 之后执行，与库存管理同挂
	// 「站点工程」目录（sort 11，排在采购入库 sort 10 之后）。
	registerSeed(Seed{
		Version:      "113-master-data-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '变更记录' AND type = 2 AND deleted_time IS NULL",
		SQL:          masterDataMenuSQL,
	})

	// 114：捆绑品选项规则（issue #20）。默认「表存在即跳过」在 products 上必然误跳过，
	// 故 CheckSQL 核对形状约束是否在位（列默认值 + 规范化 UPDATE + CHECK 三者同段执行，
	// 语句全部幂等，缺约束即整段重跑）。
	register(Migration{
		Version:   "114-product-bundle-config",
		TableName: "products",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_constraint c " +
			"JOIN pg_class t ON t.oid = c.conrelid " +
			"JOIN pg_namespace n ON n.oid = t.relnamespace " +
			"WHERE n.nspname = current_schema() AND t.relname = ? " +
			"AND c.conname = 'products_bundle_items_shape_check'",
		SQL: productBundleConfigSQL,
	})

	// 115：捆绑品 4 个权限点 + 超管策略（issue #20）。
	// 条件只看本票自己的权限点（product:bundle_%），与 082 的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "115-product-bundle-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:bundle_get', 'product:bundle_set', " +
			"'product:bundle_validate', 'product:bundle_skus')",
		SQL: productBundlePermsSQL,
	})

	// 116：捆绑配置后台菜单（issue #20）。与 084/113 同挂「站点工程」目录（sort 12）。
	registerSeed(Seed{
		Version:      "116-product-bundle-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '捆绑配置' AND type = 2 AND deleted_time IS NULL",
		SQL:          productBundleMenuSQL,
	})

	// 117：商品按品牌筛选的索引（issue #21）。products 表存在即默认跳过，
	// 故 CheckSQL 核对索引是否真的在位（缺索引即整段重跑，语句幂等）。
	register(Migration{
		Version:   "117-product-brand-index",
		TableName: "products",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_products_brand_id'",
		SQL: productBrandIndexSQL,
	})

	// 118：变体属性值的 GIN 索引（issue #25）。product_variants 表存在即默认跳过，
	// 故 CheckSQL 核对索引是否真的在位（缺索引即整段重跑，语句幂等）。
	register(Migration{
		Version:   "118-product-variant-option-index",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_product_variants_option_values'",
		SQL: productVariantOptionIndexSQL,
	})

	// 119：变体价格的索引（issue #28）。价格维度 EXISTS 与 MIN(price) 投影都只认启用变体，
	// 故建部分索引；CheckSQL 核对索引是否真的在位（缺索引即整段重跑，语句幂等）。
	register(Migration{
		Version:   "119-product-variant-price-index",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_product_variants_price_enabled'",
		SQL: productVariantPriceIndexSQL,
	})

	// 120：商品评分独立表（issue #30 修正 #29 的「评分当商品列」）。CheckSQL 核对该表是否在位
	// （缺表即整段重跑，语句幂等；DROP COLUMN 用 IF EXISTS 保证重跑安全）。
	register(Migration{
		Version:   "120-product-rating",
		TableName: "product_ratings",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?",
		SQL: productRatingSQL,
	})

	// 121：去掉商品侧库存缓存（issue #32）。CheckSQL 核对两个缓存列确已不存在
	//（缺列即整段重跑，语句全部 IF EXISTS 幂等）。
	register(Migration{
		Version:   "121-drop-stock-cache",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"AND column_name IN ('stock_total', 'stock_synced_at')",
		SQL: dropStockCacheSQL,
	})

	// 122：删缓存相关权限点与策略（issue #32）。CheckSQL 核对两个权限点确已不存在
	//（权限点还在即整段重跑，语句幂等）。
	register(Migration{
		Version:   "122-drop-cache-permissions",
		TableName: "inventory:cache_sync",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_permission " +
			"WHERE permission_code IN (?, 'inventory:cache_reconcile')",
		SQL: dropCachePermissionsSQL,
	})

	// 073：把历史库的 blueprints / blueprint_versions 对齐到 model（唯一真源）。
	// CheckSQL 表达「已对齐」条件（返回 > 0 则跳过）：三个历史残留列全部消失才算完成。
	register(Migration{
		Version:   "073-blueprint-ddl-align",
		TableName: "blueprints",
		CheckSQL: `SELECT COUNT(*) FROM information_schema.tables t
			WHERE t.table_schema = current_schema() AND t.table_name = ?
			  AND NOT EXISTS (
			      SELECT 1 FROM information_schema.columns c
			      WHERE c.table_schema = t.table_schema AND c.table_name = t.table_name
			        AND c.column_name IN ('project_id','source_hash','created_by')
			  )`,
		SQL: blueprintDDLAlignSQL,
	})

	// ---- i18n 数据层（P0：表结构 + 词条 seed，docs/06-D §13 P0/P1）----
	//
	// 055：sys_i18n 补 category/remark。sys_i18n 由 init_schema.sql 建表，
	// 默认「表存在即跳过」会误跳过，故按 category 列是否存在判定。
	register(Migration{
		Version:   "055-i18n-columns",
		TableName: "sys_i18n",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'category'",
		SQL:       i18nColumnsSQL,
	})

	// 056：sys_i18n_revision 单行资源版本号（对齐 a2 历史结构，SWR 协商用）。
	register(Migration{
		Version:   "056-i18n-revision",
		TableName: "sys_i18n_revision",
		SQL:       i18nRevisionSQL,
	})

	// 057：sys_menus 补 title_key（修现存 bug：model 已 SELECT title_key，但建表缺列）。
	// 同样按列是否存在判定，避免默认「表存在即跳过」。
	register(Migration{
		Version:   "057-sys-menus-title-key",
		TableName: "sys_menus",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'title_key'",
		SQL:       menuTitleKeySQL,
	})

	// 058：enums 全量词条 seed（zh-CN 195 行 / en-US 79 行，ON CONFLICT 幂等）。
	// ConditionSQL 以 zh-CN 行数为门槛：已灌满则跳过；新增词条时同步调大阈值即可重跑补齐。
	registerSeed(Seed{
		Version:      "058-i18n-seed-enums",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 195 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN'",
		SQL:          i18nSeedEnumsSQL,
	})

	// 059：后台外壳 shell.* 词条 seed（zh-CN 25 行 / en-US 25 行，多语言 P1 第二步）。
	// ConditionSQL 以 shell.* 的 zh-CN 行数为门槛（与 058 的 195 门槛互不干扰）：
	// 已灌满则跳过；新增 shell 词条时同步调大阈值即可重跑补齐。
	registerSeed(Seed{
		Version:      "059-i18n-seed-shell",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 25 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key LIKE 'shell.%'",
		SQL:          i18nSeedShellSQL,
	})

	// 060：访客面组件固定文案 site.component.* 词条 seed
	// （zh-CN 13 行 / en-US 13 行，多语言 P4：构建期组件文案）。
	// ConditionSQL 以 site.component.* 的 zh-CN 行数为门槛（与 058/059 门槛互不干扰）：
	// 已灌满则跳过；新增组件文案词条时同步调大阈值即可重跑补齐。
	registerSeed(Seed{
		Version:      "060-i18n-seed-site-components",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 13 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key LIKE 'site.component.%'",
		SQL:          i18nSeedSiteComponentsSQL,
	})

	// 061：page_artifacts 加 lang 维度（多语言上线第一阻塞项，docs/06-D §15.5 第 1 条）。
	// 唯一键 (page_id, version) → (page_id, version, lang)，同页多语言各占一行。
	// page_artifacts 由 002-init-builder-schema 创建，默认「表存在即跳过」会误跳过，
	// 故按 lang 列是否存在判定（与 047/049/054/055 同一手法）。
	register(Migration{
		Version:   "061-page-artifacts-lang",
		TableName: "page_artifacts",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'lang'",
		SQL:       pageArtifactsLangSQL,
	})

	// 062：page_publications（页面每语言激活状态，多语言 P3，docs/06-D §15.5 第 2 条）。
	// 解掉 pages.active_path 单值导致「Publish(en-US) 取消 /zh-CN/about 激活路由」。
	// 新表，默认「表存在即跳过」检查即可。
	register(Migration{
		Version:   "062-page-publications",
		TableName: "page_publications",
		SQL:       pagePublicationsSQL,
	})

	// 063：page_stagings（页面每语言暂存产物，多语言 P3）。
	// 解掉 pages.staged_artifact_id 单值导致「先构建两语言再逐个发布」失败。
	register(Migration{
		Version:   "063-page-stagings",
		TableName: "page_stagings",
		SQL:       pageStagingsSQL,
	})

	// 064：project_locales（站点语言清单，多语言 P3，docs/06-D §14 D10）。
	register(Migration{
		Version:   "064-project-locales",
		TableName: "project_locales",
		SQL:       projectLocalesSQL,
	})

	// 065：语言切换器容器无障碍标签词条 seed（site.component.languages.label，
	// zh-CN 1 行 / en-US 1 行，多语言 P3 前台切换器）。
	// ConditionSQL 以 site.component.languages.* 的 zh-CN 行数为门槛，
	// 与 060 的 site.component.% 门槛互不干扰（060 已灌满不会再跑）。
	registerSeed(Seed{
		Version:      "065-i18n-seed-language-switcher",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key LIKE 'site.component.languages.%'",
		SQL:          i18nSeedLanguageSwitcherSQL,
	})

	// 066：sys_translation 内容寻址翻译表（多语言 P5a，docs/06-D §7.3）。
	// 与 sys_i18n 分工见决策 F17：本表跟内容编辑走（构建器内联文本 + CMS 字段），
	// 按 (source_hash, context, lang) 寻址，改原文即自动失效。
	// 新表，默认「表存在即跳过」检查即可（CheckSQL 留空 = 按表存在判定）。
	register(Migration{
		Version:   "066-sys-translation",
		TableName: "sys_translation",
		SQL:       sysTranslationSQL,
	})

	// 067：媒体中心（02-B）在既有 sys_attachment 上落地四能力（不新建 media_* 三表）：
	// extra_info json→jsonb（含 NULL/脏数据兜底）、GIN 索引、generation 列、md5 去重索引。
	// sys_attachment 由 001-init-schema 创建，默认「表存在即跳过」必然误跳过，
	// 故按 generation 列是否存在判定（与 047/049/054/055/057/061 同一手法）。
	register(Migration{
		Version:   "067-media-center",
		TableName: "sys_attachment",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'generation'",
		SQL:       mediaCenterSQL,
	})

	// 068：PG 特性优化第一批 —— pages 块引用表达式 GIN 索引（替代 ::text LIKE 搜 JSON）
	// + 软删除部分索引（WHERE deleted_at IS NULL）。
	// pages 由 002-init-builder-schema 创建，默认「表存在即跳过」必然误跳过，
	// 故按索引名判定（与 037/038 同一手法）。
	register(Migration{
		Version:   "068-pg-jsonb-partial-index",
		TableName: "pages",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'idx_pages_blockref'",
		SQL:       pgJSONBPartialIndexSQL,
	})

	// 069：删除 6 张「设计已被取代」的空表（media_asset/_variant/_reference 与
	// global_components/_versions/_policies），并解绑保留表上指向它们的 4 个外键。
	// 本迁移是 DROP 语义，默认「表存在即跳过」正好相反，故用自定义 CheckSQL：
	// 6 张表全部不存在且 4 个外键全部已解绑时返回 1（跳过），否则执行（DROP IF EXISTS 幂等）。
	// TableName 取 media_asset 作为首个 ? 参数。
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
		SQL: dropObsoleteDesignTablesSQL,
	})

	// 070：PG 优化第二梯队 —— sys_* 软删除/状态列索引核对（第一批 068 的遗留项 ①）。
	// 逐个核对 model 里真实出现的 Where 子句后，只补 3 处「查询真的会用到、且现有索引
	// 没覆盖」的缺口（全部在媒体中心）：
	//   · idx_att_file_path_alive  —— 构建期按 file_path 反查附件（此前无任何索引）
	//   · idx_mva_file_path        —— 构建期按 file_path 反查变体（此前无任何索引）
	//   · idx_att_cat_time_alive   —— 媒体库按分类分页列表（等值列 + 排序列 + 存活谓词）
	// 其余 9 张 sys_* 表要么已有覆盖索引、要么查询根本不用该条件，逐个跳过（原因写在 SQL 里）。
	// sys_attachment / sys_media_variant 早已存在，默认「表存在即跳过」必然误跳过，
	// 故按索引名判定（与 037/038/068 同一手法）。
	// 幂等检查要求 3 个索引全部存在（缺任意一个即重跑，CREATE INDEX IF NOT EXISTS 安全）：
	// 只按其中一个判定会在「部分索引被手工删除」时误跳过。
	register(Migration{
		Version:   "070-sys-status-index-audit",
		TableName: "sys_attachment",
		// 注意：migrator.apply 固定以 TableName 作为唯一 ? 参数调用 CheckSQL，
		// 故这里必须保留恰好一个 ?（用 IN (?, 'sys_media_variant') 覆盖两张表）。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() " +
			"AND indexname IN ('idx_att_file_path_alive', 'idx_mva_file_path', 'idx_att_cat_time_alive') " +
			"AND tablename IN (?, 'sys_media_variant')",
		SQL: sysStatusIndexAuditSQL,
	})

	// 071：依赖 fan-out 的反查索引 + dependency_kind 约束扩展（PIPE-3）。
	// 两表此前无任何 Go 侧写入路径，失效标记退化为全站 UPDATE；PIPE-3 起按
	// (dependency_kind, dependency_key) 反查受影响的具体产物/页面。
	//   ① idx_page_deps_lookup / idx_pres_deps_lookup —— 主键前导列是 artifact_id，
	//      反查方向（kind+key → artifact）无索引可用，必然全表扫。
	//   ② CHECK 约束补 'i18n' / 'block' 两个实现中真实存在的构建期依赖类型
	//      （Manifest 已在用 i18n；块内联进产物）。
	// 默认「表存在即跳过」对这两张早已存在的表必然误跳过，故按索引名 + 约束定义判定：
	// 2 个索引全部存在且 2 个约束都已含 'i18n' 才算完成（部分完成时重跑，语句幂等）。
	// 注意：migrator.apply 固定以 TableName 作为唯一 ? 参数调用 CheckSQL。
	register(Migration{
		Version:   "071-dependency-fanout",
		TableName: "page_dependencies",
		// 约束判定先用 MATERIALIZED CTE 锁定本迁移自己的约束 OID 再 deparse：
		// pg_get_constraintdef 会打开约束所属的关系，若在同一查询里对整个
		// pg_constraint 调用它，会与「其它测试包并发 DROP SCHEMA」竞争（实测
		// 报 "could not open relation with OID ..."）。物化出目标 OID 后，
		// 函数只对本迁移的两张表求值，不再触碰别人的关系。
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
		SQL: dependencyFanoutSQL,
	})

	// 123：访客账号模块（issue #36）。七张表一次性建（identity / profile / preferences /
	// roles / sessions / app passwords / meta），CheckSQL 以 users 表存在判定。
	register(Migration{
		Version:   "123-user",
		TableName: "users",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: userSQL,
	})

	// 124：邮箱模块基座（issue #37）。四张表：发信账号 / 模板 / 发送日志 / 抑制名单，
	// CheckSQL 以 mail_accounts 表存在判定。
	register(Migration{
		Version:   "124-mail",
		TableName: "mail_accounts",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: mailSQL,
	})

	// 125：营销域（issue #37）。联系人 / 列表 / 成员 / 活动 / 事件五张表，
	// CheckSQL 以 mail_contacts 表存在判定。
	register(Migration{
		Version:   "125-mail-marketing",
		TableName: "mail_contacts",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: mailMarketingSQL,
	})

	// 126：邮箱模块权限点与菜单（issue #37）。
	//
	// 用 Seed 而不是 Migration：Migration 的 CheckSQL 走**参数绑定**（? 是值占位符，表名不能参数化），
	// 只能判定「表或列是否存在」；这里要判定的是「这批数据是否已插入」，Seed 的 ConditionSQL
	// 不带占位符、可写任意条件，正是干这个的。
	registerSeed(Seed{
		Version:      "126-mail-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:account_list'",
		SQL:          mailPermissionSQL,
	})

	// 127：邮件日志补活动 / 联系人关联（issue #37），以列存在判定。
	register(Migration{
		Version:   "127-mail-log-links",
		TableName: "mail_logs",
		CheckSQL:  "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'campaign_id') THEN 1 ELSE 0 END",
		SQL:       mailLogLinksSQL,
	})

	// 128：群发活动权限点与菜单（issue #37）。同样用 Seed（见 126 的说明）。
	registerSeed(Seed{
		Version:      "128-mail-campaign",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:campaign_list'",
		SQL:          mailCampaignSQL,
	})

	// 129：内置事务邮件模板（issue #37）。同样用 Seed（见 126 的说明）。
	registerSeed(Seed{
		Version:      "129-mail-templates",
		TableName:    "mail_templates",
		ConditionSQL: "SELECT COUNT(*) FROM mail_templates WHERE template_key = 'register_verify'",
		SQL:          mailBuiltinTemplatesSQL,
	})

	// 130：mail_templates.variables 改 text[]（与 tags / target_tags 同一套编解码）。
	register(Migration{
		Version:   "130-mail-template-vars",
		TableName: "mail_templates",
		CheckSQL:  "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'variables' AND data_type = 'ARRAY') THEN 1 ELSE 0 END",
		SQL:       mailTemplateVarsSQL,
	})

	// 131：自动化流程三张表（issue #38 P3）。
	register(Migration{
		Version:   "131-mail-automation",
		TableName: "mail_automations",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?",
		SQL:       mailAutomationSQL,
	})

	// 132：自动化流程权限点与菜单按钮（issue #38 P3）。用 Seed（见 126 说明）。
	registerSeed(Seed{
		Version:      "132-mail-automation",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:automation_list'",
		SQL:          mailAutomationPermSQL,
	})

	// 133：画布位置接口权限点（issue #38 P4）。用 Seed（见 126 说明）。
	registerSeed(Seed{
		Version:      "133-mail-automation-layout",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:automation_layout'",
		SQL:          mailAutomationLayoutPermSQL,
	})

	// 134：库存与采购行的商品 / 变体引用完整性。
	register(Migration{
		Version:   "134-inventory-reference-fks",
		TableName: "inventory_stocks",
		// 8 个约束必须**全部**存在才算已执行：只查其中一个的话，部分缺失时会被判成
		// 「已存在」而永久跳过，缺的那几条外键再也不会补上。SQL 本身逐条 IF NOT EXISTS，
		// 判为未执行时重跑是安全的。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM pg_constraint c WHERE (CAST(? AS text) IS NOT NULL) AND c.conname IN (" +
			"'fk_inventory_stocks_product', 'fk_inventory_purchase_lines_product'," +
			"'fk_inventory_purchase_lines_variant', 'fk_inventory_receipt_items_product'," +
			"'fk_inventory_receipt_items_variant', 'fk_inventory_movements_product'," +
			"'fk_product_price_adjustment_items_product', 'fk_product_price_adjustment_items_variant')",
		SQL: inventoryReferenceFKsSQL,
	})

	// 135：订单头 / 订单项快照 / 状态流转流水（BIZ-1 销售侧）。
	register(Migration{
		Version:   "135-order",
		TableName: "orders",
		// 三张表**全部**建好才算已执行：只查 orders 的话，中途失败会留下
		// 「orders 在、order_items 不在」却被永久跳过的状态（与 134 同因）。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name IN ('orders', 'order_items', 'order_status_logs')",
		SQL: orderTablesSQL,
	})

	// 136：订单权限点 + 超管策略（8 个权限点全部存在才算已 seed）。
	registerSeed(Seed{
		Version:   "136-order-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'order:list', 'order:get', 'order:create', 'order:status', " +
			"'order:cancel', 'order:refund', 'order:item_list', 'order:log_list')",
		SQL: orderPermsSQL,
	})
}
