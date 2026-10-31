package migrations

import "sync"

func registerAICallLog() {
	registerAICallLogOnce.Do(registerAICallLogSeed)
}

var registerAICallLogOnce sync.Once

func registerAICallLogSeed() {
	registerSeed(Seed{
		Version:      "537-ai-call-log",
		TableName:    "ai_call_log",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_call_log') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("537_ai_call_log.sql"),
	})
}
