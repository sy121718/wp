package migrations

import "sync"

// register_ai_mcp_i18n_auth.go — 「MCP 与外部访问」页新增的一处文案（546）。
//
// 为什么是**新**迁移而不是补进 545：545 的判定在已执行过的库上成立并跳过，
// 新键写进去不会被跑（AGENTS.md「迁移」一节）。种子按批切分，判据只枚举本批自己的行。
func registerAIMcpI18nAuth() {
	registerAIMcpI18nAuthOnce.Do(registerAIMcpI18nAuthSeed)
}

// registerAIMcpI18nAuthOnce 让重复调用成为空操作。
var registerAIMcpI18nAuthOnce sync.Once

// registerAIMcpI18nAuthSeed 注册 546（真正干活的那一半）。
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
