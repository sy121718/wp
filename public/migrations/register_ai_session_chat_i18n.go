package migrations

import "sync"

func registerAISessionChatI18n() {
	registerAISessionChatI18nOnce.Do(registerAISessionChatI18nSeed)
}

var registerAISessionChatI18nOnce sync.Once

func registerAISessionChatI18nSeed() {
	registerSeed(Seed{
		Version:   "519-ai-session-chat-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.ai.session.send.title', 'ai.err.sessionChatUnavailable') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("519_ai_session_chat_i18n.sql"),
	})
}
