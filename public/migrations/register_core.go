package migrations

// registerCoreSchemaAndAccess 注册「核心 schema 与早期权限/菜单 seed（001–054）」。
//
// 由 register.go 的 init() 按主题拆出：本文件只做注册，不含任何 SQL 文本，
// 注册项引用的 SQL 一律经 mustSQL 从嵌入的 .sql 取。
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

	// 业务权限 seed（权限点 + 菜单 + 超管全量策略）。
	// 执行入口：internal/routers/routes.go 路由装配时调用 RunSeeds（幂等）。
	registerSeed(Seed{
		Version:      "030-business-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('page','project','block','media','artifact','publication')",
		SQL:          mustSQL("030_business_permissions.sql"),
	})
	registerSeed(Seed{
		Version:      "031-business-permissions-superadmin",
		TableName:    "sys_casbin_rule",
		ConditionSQL: "SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v1 = '/api/page/list' AND v0 IN (SELECT CAST(id AS VARCHAR) FROM sys_admin WHERE is_admin = 1)",
		SQL:          mustSQL("031_business_permissions_superadmin.sql"),
	})

	// 插件注册表（docs/06-plugin-system.md §8：安装/版本/启停记账）。
	register(Migration{
		Version:   "040-plugin-registry",
		TableName: "plugin_registry",
		SQL:       mustSQL("040_plugin_registry.sql"),
	})

	// CMS 内容实体（docs/02-domain.md §1，0-A2 content 模块）。
	register(Migration{
		Version:   "042-content",
		TableName: "contents",
		SQL:       mustSQL("042_content.sql"),
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
		SQL:       mustSQL("046_navigation.sql"),
	})

	// 全局块自由分类（组织/筛选维度，不改引用维度）。
	// blocks 表已在 021 创建，默认幂等检查（表存在即跳过）会误跳过，
	// 故用自定义 CheckSQL 按 category 列是否存在判断。
	register(Migration{
		Version:   "047-block-category",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'category'",
		SQL:       mustSQL("047_block_category.sql"),
	})

	// 全局块复用方式维度（docs/02-D：global 引用 / template 一次性复制）。
	// blocks 表已在 021 创建，默认幂等检查会误跳过，仿 047 按 reuse_mode 列是否存在判断。
	register(Migration{
		Version:   "049-block-reuse-mode",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'reuse_mode'",
		SQL:       mustSQL("049_block_reuse_mode.sql"),
	})

	// 导航项多来源（对齐 WP 菜单：页面/文章/产品/分类/全局块 + 打开方式）。
	register(Migration{
		Version:   "054-navigation-sources",
		TableName: "navigations",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'source_type'",
		SQL:       mustSQL("054_navigation_sources.sql"),
	})

	// 插件权限 seed（权限点 + 后台菜单，docs/06）。
	registerSeed(Seed{
		Version:      "032-plugin-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module = 'plugin'",
		SQL:          mustSQL("032_plugin_permissions.sql"),
	})

	// CMS 内容权限 seed（权限点 + 超管策略，0-A2）。
	registerSeed(Seed{
		Version:      "033-content-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module = 'content'",
		SQL:          mustSQL("033_content_permissions.sql"),
	})

	// 内容模板与自动发布权限 seed（0-A2）。
	registerSeed(Seed{
		Version:      "034-content-pipeline-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('contenttemplate','presentation')",
		SQL:          mustSQL("034_content_pipeline_permissions.sql"),
	})

	// Blueprint 与 Navigation 权限 seed（0-B/0-C）。
	registerSeed(Seed{
		Version:      "036-blueprint-navigation-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('blueprint','navigation')",
		SQL:          mustSQL("036_blueprint_navigation_permissions.sql"),
	})

	// 主题（Theme）权限 seed（project 模块下 theme 能力，此前无权限点体系）。
	registerSeed(Seed{
		Version:      "035-theme-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'project:theme_%'",
		SQL:          mustSQL("035_theme_permissions.sql"),
	})

	// 主题「同工程单激活」部分唯一索引（themes 表已由 020 创建，默认表存在检查会误跳过，
	// 故用自定义 CheckSQL 按索引名判断是否存在）。
	register(Migration{
		Version:   "037-theme-active-unique",
		TableName: "themes",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'uq_themes_project_active'",
		SQL:       mustSQL("037_theme_active_unique.sql"),
	})

	// blocks 表「同工程块名大小写不敏感」唯一索引（blocks 已由 021 创建，默认表存在检查会误跳过，
	// 故用自定义 CheckSQL 按索引名判断是否存在）。
	register(Migration{
		Version:   "038-blocks-name-lower-unique",
		TableName: "blocks",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'uq_blocks_project_name_lower'",
		SQL:       mustSQL("038_blocks_name_lower_unique.sql"),
	})

	// 媒体图片变体表（thumb/medium/webp），同文件内附带 media 下载/变体接口权限点 seed。
	// Migration 负责建表；Seed 由 RunSeeds 兜底保证权限点存在
	// （DDL IF NOT EXISTS / INSERT NOT EXISTS 均幂等，重复执行安全）。
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

	// 管理面六领域权限 seed（权限点 + 超管策略）：六领域 API 挂 Casbin 但此前从未 seed，
	// 超管访问 /api/role/* 等也被拒。幂等，重启或 RunSeeds 时生效。
	registerSeed(Seed{
		Version:      "050-admin-domains-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE module IN ('admin','role','permission','menu','dept','datarule')",
		SQL:          mustSQL("050_admin_domains_permissions.sql"),
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
		// apply() 的判定是「count > 0 → 跳过」，所以这里必须表达「FK 不存在才算已完成」；
		// 直接 COUNT(FK) 会把「FK 存在（正需要删）」判成已完成，DROP 永不执行。
		CheckSQL:  "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM pg_constraint WHERE conname = 'page_routes_page_id_fkey' AND conrelid = ?::regclass",
		SQL:       mustSQL("053_drop_route_page_fk.sql"),
	})

	registerSeed(Seed{
		Version:      "052-menu-icons",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND (icon IS NULL OR icon = '' OR icon LIKE 'i-ep:%')",
		SQL:          mustSQL("052_menu_icons.sql"),
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
		SQL: mustSQL("051_superadmin_all_policies.sql"),
	})
}
