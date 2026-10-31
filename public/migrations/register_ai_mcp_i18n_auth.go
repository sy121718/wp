package migrations

import "sync"

func registerAIMcpI18nAuth() {
	registerAIMcpI18nAuthOnce.Do(registerAIMcpI18nAuthSeed)
}

var registerAIMcpI18nAuthOnce sync.Once

func registerAIMcpI18nAuthSeed() {
	registerSeed(Seed{
		Version:   "546-ai-mcp-i18n-auth",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key = 'admin.ai.mcp.endpoint.auth' " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("546_ai_mcp_i18n_auth.sql"),
	})
}
