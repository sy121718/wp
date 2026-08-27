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
}
