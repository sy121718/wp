package migrations

import "sync"

func registerAICallLogCachedTokens() {
	registerAICallLogCachedTokensOnce.Do(registerAICallLogCachedTokensSeed)
}

var registerAICallLogCachedTokensOnce sync.Once

func registerAICallLogCachedTokensSeed() {
	registerSeed(Seed{
		Version:   "566-ai-call-log-cached-tokens",
		TableName: "ai_call_log",

		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_name = 'ai_call_log' AND column_name IN ('cached_tokens', 'cached_reported')) = 2 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("566_ai_call_log_cached_tokens.sql"),
	})
}
