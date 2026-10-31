package migrations

import "sync"

func registerAISessionCallsI18n() {
	registerAISessionCallsI18nOnce.Do(registerAISessionCallsI18nSeed)
}

var registerAISessionCallsI18nOnce sync.Once

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
