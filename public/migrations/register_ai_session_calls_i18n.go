package migrations

import "sync"

// register_ai_session_calls_i18n.go — 会话行悬浮卡「最近调用」的词条（538）。
//
// 判据只枚举本批自己的 key（上界封闭）：8 个 key × 2 语言 = 16 条元组，两个语言都在才算完成。
func registerAISessionCallsI18n() {
	registerAISessionCallsI18nOnce.Do(registerAISessionCallsI18nSeed)
}

// registerAISessionCallsI18nOnce 让重复调用成为空操作。
var registerAISessionCallsI18nOnce sync.Once

// registerAISessionCallsI18nSeed 注册 538（真正干活的那一半）。
func registerAISessionCallsI18nSeed() {
	registerSeed(Seed{
		Version:   "538-ai-session-calls-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.session.calls.title', 'admin.ai.session.calls.total', " +
			"'admin.ai.session.calls.unit', 'admin.ai.session.calls.col.time', " +
			"'admin.ai.session.calls.col.latency', 'admin.ai.session.calls.col.tokens', " +
			"'admin.ai.session.calls.col.status', 'admin.ai.session.calls.ok') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 16 THEN 1 ELSE 0 END",
		SQL: mustSQL("538_ai_session_calls_i18n.sql"),
	})
}
