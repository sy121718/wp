package migrations

import "sync"

// register_ai_session_tools_i18n.go — AI 会话「工具调用」词条（539）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：挑两个**代表 key**（各自的中英两条都必须在），
// 本批共 3 个 key × 2 语言 = 6 条元组，用「代表 key 的语言齐全」当门槛 ——
// 不枚举全部 6 条，也不用全库计数 / LIKE 前缀（存量库永远满足 → 补词条的那条迁移
// 永远不会执行，迁移 494 记过这个坑）。
//
// 偏差方向刻意选「宁可重跑，不可静默跳过」：代表 key 齐全即视为整批已完成；
// ON CONFLICT DO NOTHING 保证重跑幂等。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。
func registerAISessionToolsI18n() {
	registerAISessionToolsI18nOnce.Do(registerAISessionToolsI18nSeed)
}

// registerAISessionToolsI18nOnce 让重复调用成为空操作。
var registerAISessionToolsI18nOnce sync.Once

// registerAISessionToolsI18nSeed 注册 539（真正干活的那一半）。
func registerAISessionToolsI18nSeed() {
	registerSeed(Seed{
		Version:   "539-ai-session-tools-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('ai.err.sessionToolRoundsExceeded', 'ai.err.toolForbidden') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("539_ai_session_tools_i18n.sql"),
	})
}
