package migrations

import "sync"

func registerAIToolCallLog() {
	registerAIToolCallLogOnce.Do(registerAIToolCallLogSeed)
}

var registerAIToolCallLogOnce sync.Once

func registerAIToolCallLogSeed() {
	registerSeed(Seed{
		Version:      "541-ai-tool-call-log",
		TableName:    "ai_tool_call_log",
		ConditionSQL: "SELECT CASE WHEN to_regclass('ai_tool_call_log') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:          mustSQL("541_ai_tool_call_log.sql"),
	})
}
