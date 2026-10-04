package migrations

import "sync"

// register_ai_mcp_i18n.go — MCP 与外部访问页的词条（545）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：挑两个**代表 key**（各自的中英两条都必须在），
// 本批 39 个 key × 2 语言 = 78 条元组，用「代表 key 的语言齐全」当门槛（挑两个只在本文件出现的键：
// help.body 与 tool.hint —— 它们在本次之前只存在于运行库、仓库侧无迁移提供，
// 存量库靠 ON CONFLICT 幂等，新库靠这条迁移补齐）。
// INSERT 侧 ON CONFLICT DO NOTHING，重跑幂等。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerAIMcpI18n() {
	registerAIMcpI18nOnce.Do(registerAIMcpI18nSeed)
}

// registerAIMcpI18nOnce 让重复调用成为空操作。
var registerAIMcpI18nOnce sync.Once

// registerAIMcpI18nSeed 注册 545（真正干活的那一半）。
func registerAIMcpI18nSeed() {
	registerSeed(Seed{
		Version:   "545-ai-mcp-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.mcp.help.body', 'admin.ai.mcp.tool.hint') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("545_ai_mcp_i18n.sql"),
	})
}
