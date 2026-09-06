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
}
