package migrations

import "sync"

// 542-ai-access-token 是纯 DDL，登记为迁移（原因同 511）。
func registerAIAccessToken() {
	registerAIAccessTokenOnce.Do(registerAIAccessTokenMigration)
}

var registerAIAccessTokenOnce sync.Once

func registerAIAccessTokenMigration() {
	register(Migration{
		Version:   "542-ai-access-token",
		TableName: "ai_access_token",
		CheckSQL:  "SELECT CASE WHEN to_regclass('ai_access_token') IS NOT NULL AND (SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'ai_access_token' AND indexname IN ('idx_ai_access_token_prefix', 'uq_ai_access_token_hash', 'idx_ai_access_token_user')) = 3 THEN 1 ELSE 0 END",
		SQL:       mustSQL("542_ai_access_token.sql"),
	})
}
