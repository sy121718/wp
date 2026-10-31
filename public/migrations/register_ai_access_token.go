package migrations

import "sync"

func registerAIAccessToken() {
	registerAIAccessTokenOnce.Do(registerAIAccessTokenSeed)
}

var registerAIAccessTokenOnce sync.Once

func registerAIAccessTokenSeed() {
	registerSeed(Seed{
		Version:      "542-ai-access-token",
		TableName:    "ai_access_token",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_access_token') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("542_ai_access_token.sql"),
	})
}
