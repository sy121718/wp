package migrations

import "sync"

func registerAIMcpErrorTextI18n() {
	registerAIMcpErrorTextI18nOnce.Do(registerAIMcpErrorTextI18nSeed)
}

var registerAIMcpErrorTextI18nOnce sync.Once

func registerAIMcpErrorTextI18nSeed() {
	registerSeed(Seed{
		Version:   "711-ai-mcp-error-text-i18n",
		TableName: "sys_i18n",
		// 门槛只枚举本批自己的代表 key（4 个 (item_key, lang) 元组全齐才算完成）：
		// 用全库计数或 LIKE 前缀会让存量库永远满足，补词条的那条迁移就再也不会执行。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.ai.mcp.error.tokenInvalid', 'zh-CN'), " +
			"('admin.ai.mcp.error.tokenInvalid', 'en-US'), " +
			"('admin.ai.mcp.error.toolRunFailed', 'zh-CN'), " +
			"('admin.ai.mcp.error.toolRunFailed', 'en-US'))",
		SQL: mustSQL("711_ai_mcp_error_text_i18n.sql"),
	})
}
