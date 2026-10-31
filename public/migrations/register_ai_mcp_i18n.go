package migrations

import "sync"

func registerAIMcpI18n() {
	registerAIMcpI18nOnce.Do(registerAIMcpI18nSeed)
}

var registerAIMcpI18nOnce sync.Once

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
