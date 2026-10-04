package migrations

import "sync"

// register_ai_session_usage_hover_i18n.go — 列表行 token 悬浮卡的词条（530）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：本批 2 个 key × 2 语言 = 4 条元组，
// 要求两个 key 的中英都在才视为完成 —— 只判一个 key 时，另一条漏插了也永远补不上。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerAISessionUsageHoverI18n() {
	registerAISessionUsageHoverI18nOnce.Do(registerAISessionUsageHoverI18nSeed)
}

// registerAISessionUsageHoverI18nOnce 让重复调用成为空操作。
var registerAISessionUsageHoverI18nOnce sync.Once

// registerAISessionUsageHoverI18nSeed 注册 530（真正干活的那一半）。
func registerAISessionUsageHoverI18nSeed() {
	registerSeed(Seed{
		Version:   "530-ai-session-usage-hover-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.session.usage.breakdown', 'admin.ai.session.usage.unrecorded') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("530_ai_session_usage_hover_i18n.sql"),
	})
}
