package migrations

import "sync"

func registerAIEventUser() {
	registerAIEventUserOnce.Do(registerAIEventUserSeed)
}

var registerAIEventUserOnce sync.Once

func registerAIEventUserSeed() {
	registerSeed(Seed{
		Version:   "535-ai-event-user",
		TableName: "ai_event",
		ConditionSQL: `SELECT CASE WHEN EXISTS (` +
			`SELECT 1 FROM information_schema.columns ` +
			`WHERE table_name = 'ai_event' AND column_name = 'user_id') ` +
			`THEN 1 ELSE 0 END`,
		SQL: mustSQL("535_ai_event_user.sql"),
	})
}
