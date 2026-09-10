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

//go:embed 073_blueprint_ddl_align.sql
var blueprintDDLAlignSQL string

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
}
