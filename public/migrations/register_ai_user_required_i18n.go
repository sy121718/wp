package migrations

import "sync"

// register_ai_user_required_i18n.go — AI 调用第一关卡的文案词条（536）。
//
// 判据只枚举本批自己的 key（上界封闭）：1 个 key × 2 语言 = 2 条元组，两个语言都在才算完成。
func registerAIUserRequiredI18n() {
	registerAIUserRequiredI18nOnce.Do(registerAIUserRequiredI18nSeed)
}

// registerAIUserRequiredI18nOnce 让重复调用成为空操作。
var registerAIUserRequiredI18nOnce sync.Once

// registerAIUserRequiredI18nSeed 注册 536（真正干活的那一半）。
func registerAIUserRequiredI18nSeed() {
	registerSeed(Seed{
		Version:   "536-ai-user-required-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key = 'ai.err.userRequired' " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("536_ai_user_required_i18n.sql"),
	})
}
