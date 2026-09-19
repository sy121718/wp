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
	// 判定必须是「030 要插的这批权限点是否已经存在」，而不是「这六个模块下有没有任何权限点」。
	// 用后者会漏：后续迁移（077/079/151/183…）会先给同一批模块补点，RunSeeds 在**所有结构
	// 迁移之后**才跑，于是全新库上这个宽条件已经为真，030 被整体跳过 —— 29 条基础权限点
	// 从未插入。表现是全新部署时 page / project / block / media 的基础接口连超管都 403，
	// 而渐进演进的老库因为 030 跑在那些迁移之前一直正常（2026-09-16 由 CI 的全新库抓到，
	// check-permission-gaps.sh 报出 29 条缺口，本地老库全绿 —— 这正是它接进 CI 的价值）。
	// 取每个模块的第一条作代表：030 是原子插入，代表齐全即整批在；万一判定偏严也只是
	// 多执行一次，SQL 自带 NOT EXISTS 守卫，不会重复插入。
	registerSeed(Seed{
		Version:   "030-business-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN " +
			"('page:list','project:list','block:list','media:list','artifact:detail','publication:receipts_pending')",
		SQL: mustSQL("030_business_permissions.sql"),
	})
	// 默认超管账号（030a）：sys_admin 此前没有任何创建路径，全新部署的库是空的 ——
	// 没人能登录、031 也没有授权对象。必须排在 031 之前（031 从 is_admin = 1 取授权对象）。
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
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM pg_constraint WHERE conname = 'page_routes_page_id_fkey' AND conrelid = ?::regclass",
		SQL:      mustSQL("053_drop_route_page_fk.sql"),
	})

	registerSeed(Seed{
		Version:      "052-menu-icons",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND (icon IS NULL OR icon = '' OR icon LIKE 'i-ep:%')",
		SQL:          mustSQL("052_menu_icons.sql"),
	})

	// 224：后台导航菜单收口 —— 侧栏改由 sys_menus 驱动（唯一真源）。
	//
	// 侧栏此前读的是 dashboard 包里硬编码的 navConfig，与这张表双份漂移
	// （路径前缀都不同，交集只剩 1 条），且漏掉十几个已存在的页面。
	// 现在 navConfig 已删除，本 seed 把表的菜单树修正成可渲染形态。
	//
	// ConditionSQL 表达「收口已完成」：6 个分组目录 + 仪表盘全部就位才跳过。
	// 不用「仪表盘存在」单条判定 —— 那条最容易先成功，中途失败会让半成品数据
	// 被误判为已完成而永不修复；本条件在部分缺失时返回 0 → 重跑补齐（SQL 幂等）。
	registerSeed(Seed{
		Version:   "224-admin-nav-menu-rebuild",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 7 THEN 1 ELSE 0 END FROM sys_menus " +
			"WHERE deleted_at IS NULL AND (" +
			"(type = 1 AND title IN ('管理', '内容', '商品与库存', '交易', '站点', '系统')) " +
			"OR (type = 2 AND path = '/admin'))",
		SQL: mustSQL("224_admin_nav_menu_rebuild.sql"),
	})

	// 225：主题架构简化 —— 删除主题包导入导出权限点与「系统页面」菜单（VIS-014 下线）。
	//
	// 配套改动：220 / 221 / 140 三个 seed 已连注册带 SQL 注销（否则存在性判定会把
	// 删掉的数据重新灌回）；槽位页面与 page:site_slot_* 权限点保留，运营入口改在
	// 主题管理页顶部（admin/theme.html）。
	// CheckSQL 表达「清理已完成」：三类目标行全为 0 才跳过（条件删除，天然幂等）。
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

	// 226：数据规则配置校验词条（域白名单收口：字段/操作符越界的明确报错）。
	// 判定限定在这 4 个 key 上，不用「全库 zh-CN 行数」当门槛 —— 其它迁移的 seed 会污染计数，
	// 让判定恒为「已灌满」而静默跳过（058 就是这个形状，详见 226 文件头注释）。
	registerSeed(Seed{
		Version:   "226-i18n-seed-datarule-errors",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('ErrRuleConfigInvalid', 'ErrRuleFieldNotAllowed', " +
			"'ErrRuleLogicNotAllowed', 'ErrRuleOpNotAllowed')",
		SQL: mustSQL("226_i18n_seed_datarule_errors.sql"),
	})

	// 227：抽屉表单的通用操作词条（取消 / 保存）—— 列表页新建/编辑统一进抽屉后用同一对文案，
	// 不再逐页复制同义词条。判定同样限定在自己的 key 上（理由见 226）。
	registerSeed(Seed{
		Version:   "227-i18n-seed-common-actions",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN ('admin.common.action.cancel', 'admin.common.action.save', " +
			"'admin.common.action.edit', 'admin.common.action.create')",
		SQL: mustSQL("227_i18n_seed_common_actions.sql"),
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

	// 267：publication_receipts 的 pending 部分索引。
	//
	// 回执收敛（page 侧 ConvergePendingReceipts）与 pending 计数都在 receipt_state = 'pending'
	// 上做等值过滤并按 create_time 排序，而这条查询绝大多数时刻是空转 —— 没有索引就是全表扫描，
	// 成本随「只增不删」的回执表一起长。回执表已由 002 创建，默认的表存在检查会误跳过，
	// 故仿 037/038 按索引名判断（同样限定 current_schema()，否则残留 schema 的同名索引会误判）。
	register(Migration{
		Version:   "267-publication-receipts-pending-index",
		TableName: "publication_receipts",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'idx_publication_receipts_pending'",
		SQL:       mustSQL("267_publication_receipts_pending_index.sql"),
	})

	// 281：presentation_instances 实例级文档覆盖列（docs/04-C-instance-override.md，方案 B）。
	//
	// 商品级可视化自定义：override_document 非空 = 实例发布/重建以此文档为准（binding
	// 照常解析，实体数据更新不丢自定义）；NULL = 跟随模板（既有行为零回归）。
	// presentation_instances 已由 002 创建，默认表存在检查会误跳过，
	// 仿 047/049 按列是否存在判断（限定 current_schema()）。
	register(Migration{
		Version:   "281-presentation-override-document",
		TableName: "presentation_instances",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'override_document'",
		SQL:       mustSQL("281_presentation_override_document.sql"),
	})

	// 282：presentation_instances 渲染模式（商品页双轨，docs/04-C-instance-override.md）。
	//
	// template（默认）= 跟随绑定模板（现状零回归）；document = 该商品独立文档（override_document）。
	// 显式成列的原因：模板更新的 stale 分流要在 SQL 里可判定，且「改了又改回去」这类
	// 状态用「override 是否为空」推断会漂移。presentation_instances 已由 002 创建，
	// 默认表存在检查会误跳过，仿 281 按列是否存在判断（限定 current_schema()）。
	register(Migration{
		Version:   "282-presentation-render-mode",
		TableName: "presentation_instances",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'render_mode'",
		SQL:       mustSQL("282_presentation_render_mode.sql"),
	})

	// 269：navigation 内部错误归口文案（审计 CQ-009）。
	// enums 的值就是 i18n key（navigationenums.ErrInternal）—— 不 seed，响应层 translate
	// 未命中会把 key 原样返回给前端。判据按本批自己的 key 计数：用总量会被同期其它批次
	// 的行满足而静默跳过（060 / 221 / 268 都记过这个坑）。
	registerSeed(Seed{
		Version:      "269-navigation-err-internal",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'navigation.err.internal'",
		SQL:          mustSQL("269_navigation_err_internal_i18n.sql"),
	})

	// 277：批量 id 超限的受控提示词条（shell.err.bulkIdsTooMany，中英各一行）。
	//
	// 与 shell.ErrBulkIDsTooMany / *shell.BulkIDsError 同批落地：受控性由**类型**表达之后，
	// 对外文案需要一个带 key 的出口 —— shell.BulkIDsFacingText 按当前语言取这条词条，
	// 两个 %s 依次是上限与本次条数（「当前 N 项」只有 shell 知道，这正是模块别重算的理由）。
	// 判定限定在本批自己的 key 上：用全库行数会被同期其它批次的行满足而静默跳过（理由见 226）。
	registerSeed(Seed{
		Version:      "277-shell-bulk-ids-too-many-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'shell.err.bulkIdsTooMany'",
		SQL:          mustSQL("277_i18n_seed_shell_bulk_ids.sql"),
	})

	// 280：库存页成功回执的受控文案（admin.inventory.actionDone，中英各一行）。
	//
	// 与读侧收口同批落地：库存页的写入口成功时回带 ?ok=1 / ?done=1，模板直接渲染该值，
	// 于是页面上出现的是裸「1」。读侧现在把它收敛成一句翻译过的固定文案
	//（inventory_page_handle.go 的 inventoryNoticeSuccess），本词条就是那句话。
	// 判定限定在本批自己的 key 上：用全库行数会被同期其它批次的行满足而静默跳过（理由见 226）。
	registerSeed(Seed{
		Version:      "280-inventory-action-done-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.inventory.actionDone'",
		SQL:          mustSQL("280_inventory_action_done_i18n.sql"),
	})
}
