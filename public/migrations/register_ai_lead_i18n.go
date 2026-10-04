package migrations

import "sync"

// register_ai_lead_i18n.go — 统一入口的页头说明词条（533）。
//
// 判据只枚举本批自己的 key（上界封闭）：1 个 key × 2 语言 = 2 条元组，两个语言都在才算完成。
func registerAILeadI18n() {
	registerAILeadI18nOnce.Do(registerAILeadI18nSeed)
}

// registerAILeadI18nOnce 让重复调用成为空操作。
var registerAILeadI18nOnce sync.Once

// registerAILeadI18nSeed 注册 533（真正干活的那一半）。
func registerAILeadI18nSeed() {
	registerSeed(Seed{
		Version:   "533-ai-lead-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key = 'admin.ai.lead' " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("533_ai_lead_i18n.sql"),
	})
}
