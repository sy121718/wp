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

//go:embed 043_content_template.sql
var contentTemplateSQL string

//go:embed 044_presentation.sql
var presentationSQL string

//go:embed 034_content_pipeline_permissions.sql
var contentPipelinePermSQL string

//go:embed 045_blueprint.sql
var blueprintSQL string

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

	// 内容结构模板（docs/02-domain.md §2，0-A2 contenttemplate 模块）。
	register(Migration{
		Version:   "043-content-template",
		TableName: "content_templates",
		SQL:       contentTemplateSQL,
	})

	// 自动发布实例与快照（docs/02-domain.md §3，0-A2 presentation 模块）。
	register(Migration{
		Version:   "044-presentation",
		TableName: "presentation_instances",
		SQL:       presentationSQL,
	})

	// Page 初始化工具 Blueprint（docs/02 §1.2，0-B）。
	register(Migration{
		Version:   "045-blueprint",
		TableName: "blueprints",
		SQL:       blueprintSQL,
	})

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

	registerSeed(Seed{
		Version:      "051-superadmin-all-policies",
		TableName:    "sys_casbin_rule",
		ConditionSQL: "SELECT COUNT(*) FROM sys_casbin_rule r JOIN sys_permission p ON r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code WHERE r.ptype = 'p' AND r.v0 IN (SELECT CAST(id AS VARCHAR) FROM sys_admin WHERE is_admin = 1)",
		SQL:          superadminAllPoliciesSQL,
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
}
