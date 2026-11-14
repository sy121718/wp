package migrations

import "sync"

// 541-ai-tool-call-log 是纯 DDL，登记为迁移（原因同 511）。
func registerAIToolCallLog() {
	registerAIToolCallLogOnce.Do(registerAIToolCallLogMigration)
}

var registerAIToolCallLogOnce sync.Once

func registerAIToolCallLogMigration() {
	register(Migration{
		Version:   "541-ai-tool-call-log",
		TableName: "ai_tool_call_log",
		CheckSQL:  "SELECT CASE WHEN to_regclass('ai_tool_call_log') IS NOT NULL THEN 1 ELSE 0 END",
		SQL:       mustSQL("541_ai_tool_call_log.sql"),
	})
}
