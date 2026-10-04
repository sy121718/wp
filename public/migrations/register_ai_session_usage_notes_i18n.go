package migrations

import "sync"

// register_ai_session_usage_notes_i18n.go — 会话详情抽屉的 token 口径词条（528）。
//
// 判据按本批自己的对象逐条枚举（上界封闭）：挑两个**代表 key**（各自的中英两条都必须在），
// 本批共 4 个 key × 2 语言 = 8 条元组，用「代表 key 的语言齐全」当门槛 ——
// 不枚举全部 8 条，也不用全库计数 / LIKE 前缀（存量库永远满足 → 补词条的那条迁移
// 永远不会执行，迁移 494 记过这个坑）。
//
// 偏差方向刻意选「宁可重跑，不可静默跳过」：代表 key 齐全即视为整批已完成；
// ON CONFLICT DO NOTHING 保证重跑幂等。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerAISessionUsageNotesI18n() {
	registerAISessionUsageNotesI18nOnce.Do(registerAISessionUsageNotesI18nSeed)
}

// registerAISessionUsageNotesI18nOnce 让重复调用成为空操作。
var registerAISessionUsageNotesI18nOnce sync.Once

// registerAISessionUsageNotesI18nSeed 注册 528（真正干活的那一半）。
func registerAISessionUsageNotesI18nSeed() {
	registerSeed(Seed{
		Version:   "528-ai-session-usage-notes-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.session.usage.total', 'admin.ai.session.usage.compactNote') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("528_ai_session_usage_notes_i18n.sql"),
	})
}
