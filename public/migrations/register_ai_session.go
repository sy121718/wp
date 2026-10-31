package migrations

import "sync"

func registerAISession() {
	registerAISessionOnce.Do(registerAISessionSeed)
}

var registerAISessionOnce sync.Once

func registerAISessionSeed() {
	registerSeed(Seed{
		Version:      "512-ai-session",
		TableName:    "ai_session",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_session') IS NOT NULL AND to_regclass('ai_event') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("512_ai_session.sql"),
	})
}
