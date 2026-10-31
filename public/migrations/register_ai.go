package migrations

import "sync"

func registerAIProvider() {
	registerAIProviderOnce.Do(registerAIProviderSeed)
}

var registerAIProviderOnce sync.Once

func registerAIProviderSeed() {
	registerSeed(Seed{
		Version:      "511-ai-provider",
		TableName:    "ai_provider",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_provider') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("511_ai_provider.sql"),
	})
}
